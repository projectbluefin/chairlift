package sbom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// artifactType is the referrer type the SBOM is attached under. What comes
// back is not necessarily SPDX — see Parse.
const artifactType = "application/vnd.spdx+json"

// maxBlobBytes caps the SBOM download. The real Bluefin blob is ~22 MB of
// JSON describing 4,500 packages, so the cap is generous but present: this
// is an unauthenticated response being read into memory in a GUI process.
const maxBlobBytes = 128 << 20

// RegistryClient fetches an image's attached SBOM.
type RegistryClient struct {
	// HTTP is the transport used for every request. Leaving it nil uses a
	// client with a timeout rather than http.DefaultClient, which has none.
	HTTP *http.Client
}

// httpClient returns the client to use.
func (c *RegistryClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// Fetch returns the SBOM attached to ref, which is a full image reference
// such as ghcr.io/ublue-os/bluefin:stable.
//
// Discovery has two steps because the OCI referrers API is not universally
// implemented. Verified against GHCR on 2026-08-17:
// `/v2/ublue-os/bluefin/referrers/<digest>` returns 404, while the
// specification's fallback tag — the digest rewritten as `sha256-<hex>` —
// returns an index whose entries carry the artifact types. A client that
// only calls the referrers API finds no SBOM on the largest registry
// publishing these images, and reports "no changelog available" rather than
// an error.
func (c *RegistryClient) Fetch(ctx context.Context, ref string) ([]byte, error) {
	registry, repository, reference, err := SplitReference(ref)
	if err != nil {
		return nil, err
	}

	token, err := c.token(ctx, registry, repository)
	if err != nil {
		return nil, err
	}

	digest, err := c.manifestDigest(ctx, registry, repository, reference, token)
	if err != nil {
		return nil, err
	}

	manifest, err := c.sbomManifest(ctx, registry, repository, digest, token)
	if err != nil {
		return nil, err
	}

	return c.blob(ctx, registry, repository, manifest, token)
}

// SplitReference splits a full image reference into its registry, repository
// path, and tag or digest.
func SplitReference(ref string) (registry, repository, reference string, err error) {
	remainder := ref
	if at := strings.Index(remainder, "@"); at >= 0 {
		reference = remainder[at+1:]
		remainder = remainder[:at]
	}

	slash := strings.Index(remainder, "/")
	if slash < 0 {
		return "", "", "", fmt.Errorf("image reference %q has no registry", ref)
	}
	registry = remainder[:slash]
	repository = remainder[slash+1:]

	if reference == "" {
		if colon := strings.LastIndex(repository, ":"); colon >= 0 {
			reference = repository[colon+1:]
			repository = repository[:colon]
		} else {
			reference = "latest"
		}
	}

	if registry == "" || repository == "" {
		return "", "", "", fmt.Errorf("image reference %q is not registry/repository:tag", ref)
	}
	return registry, repository, reference, nil
}

// token obtains an anonymous pull token. A registry that needs no token
// returns an empty string rather than an error, so the caller's requests go
// out unauthenticated.
func (c *RegistryClient) token(ctx context.Context, registry, repository string) (string, error) {
	endpoint := fmt.Sprintf("https://%s/token?service=%s&scope=%s",
		registry, url.QueryEscape(registry),
		url.QueryEscape("repository:"+repository+":pull"))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("requesting a pull token from %s: %w", registry, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", nil
	}

	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil {
		return "", nil
	}
	if body.Token != "" {
		return body.Token, nil
	}
	return body.AccessToken, nil
}

// manifestAccept lists every manifest media type the registry may answer a
// tag with.
const manifestAccept = "application/vnd.oci.image.index.v1+json," +
	"application/vnd.oci.image.manifest.v1+json," +
	"application/vnd.docker.distribution.manifest.list.v2+json," +
	"application/vnd.docker.distribution.manifest.v2+json"

// manifestDigest resolves a tag to the digest its referrers hang off. The
// digest comes from the response header rather than from hashing the body,
// because that is what the registry itself will accept back.
func (c *RegistryClient) manifestDigest(ctx context.Context, registry, repository, reference, token string) (string, error) {
	if strings.HasPrefix(reference, "sha256:") {
		return reference, nil
	}

	endpoint := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, reference)
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", manifestAccept)
	setToken(request, token)

	response, err := c.httpClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", reference, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolving %s: registry returned %s", reference, response.Status)
	}
	digest := response.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("resolving %s: registry sent no content digest", reference)
	}
	return digest, nil
}

// descriptor is one entry in an index, and one layer in a manifest.
type descriptor struct {
	MediaType    string `json:"mediaType"`
	ArtifactType string `json:"artifactType"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
}

type indexDocument struct {
	Manifests []descriptor `json:"manifests"`
	Layers    []descriptor `json:"layers"`
}

// sbomManifest finds the SBOM referrer's manifest digest, trying the
// referrers API first and the fallback tag second.
func (c *RegistryClient) sbomManifest(ctx context.Context, registry, repository, digest, token string) (string, error) {
	endpoint := fmt.Sprintf("https://%s/v2/%s/referrers/%s", registry, repository, digest)
	index, err := c.fetchIndex(ctx, endpoint, token)
	if err == nil {
		if found, ok := selectSBOM(index.Manifests); ok {
			return found, nil
		}
	}

	fallback := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository,
		strings.Replace(digest, ":", "-", 1))
	index, err = c.fetchIndex(ctx, fallback, token)
	if err != nil {
		return "", err
	}
	if found, ok := selectSBOM(index.Manifests); ok {
		return found, nil
	}
	return "", fmt.Errorf("no %s artifact is attached to %s", artifactType, digest)
}

// selectSBOM picks the SBOM referrer out of an index's entries.
func selectSBOM(manifests []descriptor) (string, bool) {
	for _, manifest := range manifests {
		if manifest.ArtifactType == artifactType && manifest.Digest != "" {
			return manifest.Digest, true
		}
	}
	return "", false
}

func (c *RegistryClient) fetchIndex(ctx context.Context, endpoint, token string) (indexDocument, error) {
	var index indexDocument

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return index, err
	}
	request.Header.Set("Accept", manifestAccept)
	setToken(request, token)

	response, err := c.httpClient().Do(request)
	if err != nil {
		return index, fmt.Errorf("fetching %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return index, fmt.Errorf("fetching %s: registry returned %s", endpoint, response.Status)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBlobBytes)).Decode(&index); err != nil {
		return index, fmt.Errorf("parsing %s: %w", endpoint, err)
	}
	return index, nil
}

// blob downloads the SBOM document the referrer manifest points at. Its
// layer is advertised as a tar, but GHCR serves the JSON document directly
// under that media type, so the bytes are handed to Parse as they arrive
// rather than being unpacked.
func (c *RegistryClient) blob(ctx context.Context, registry, repository, manifestDigest, token string) ([]byte, error) {
	endpoint := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, manifestDigest)
	manifest, err := c.fetchIndex(ctx, endpoint, token)
	if err != nil {
		return nil, err
	}
	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("SBOM manifest %s has no layers", manifestDigest)
	}

	layerDigest := manifest.Layers[0].Digest
	blobURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repository, layerDigest)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil)
	if err != nil {
		return nil, err
	}
	setToken(request, token)

	response, err := c.httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("downloading the SBOM: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading the SBOM: registry returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBlobBytes))
	if err != nil {
		return nil, err
	}
	if err := verifyDigest(layerDigest, data); err != nil {
		return nil, err
	}
	return data, nil
}

// verifyDigest checks that data hashes to the content-addressed digest it was
// fetched by. Every earlier step of the referrer chain trusts what the
// registry sends over TLS; the blob is the payload that is actually parsed
// and rendered, so a substitution here must be caught rather than displayed.
// Only sha256 is accepted — an unrecognized algorithm is a refusal, not a
// pass-through, because an unverifiable digest verifies nothing.
func verifyDigest(digest string, data []byte) error {
	encoded, ok := strings.CutPrefix(digest, "sha256:")
	if !ok {
		return fmt.Errorf("SBOM layer digest %q is not a sha256 digest", digest)
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), encoded) {
		return fmt.Errorf("SBOM blob does not match its digest %s", digest)
	}
	return nil
}

func setToken(request *http.Request, token string) {
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}
