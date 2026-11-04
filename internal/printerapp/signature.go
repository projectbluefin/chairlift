package printerapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// ErrSignatureUnverified means no unit may be installed or refreshed.
var ErrSignatureUnverified = errors.New("printer image signature could not be verified")

var runCosign = execCosign
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func execCosign(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	// Keep stderr separate: only verified payloads from stdout are parsed.
	output, err := exec.CommandContext(ctx, "cosign", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("cosign verify: %w", err)
	}
	return output, nil
}

// verifiedImage asks cosign to check the requested reference against this
// family's publishing workflow, then pins the digest in its verified payload.
// Never resolve the tag a second time: it may move after verification.
func verifiedImage(ctx context.Context, family Family) (string, error) {
	known := false
	for _, candidate := range families {
		known = known || family.ID == candidate.ID
	}
	if !known {
		return "", fmt.Errorf("%w: unknown family %q", ErrSignatureUnverified, family.ID)
	}
	image := family.Image
	repository := strings.SplitN(image, "@", 2)[0]
	if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
		repository = repository[:colon]
	}
	if repository == "" || strings.ContainsAny(image, "\r\n\t ") || strings.HasPrefix(image, "-") {
		return "", fmt.Errorf("%w: invalid image reference", ErrSignatureUnverified)
	}
	identity := `^https://github\.com/projectbluefin/` + family.ID + `-printer-app/\.github/workflows/registry-actions\.yml@refs/heads/main$`
	output, err := runCosign(ctx, "verify", "--output=json", "--certificate-oidc-issuer", "https://token.actions.githubusercontent.com", "--certificate-identity-regexp", identity, image)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSignatureUnverified, err)
	}
	var signatures []struct {
		Critical struct {
			Image struct {
				Digest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
	}
	if err := json.Unmarshal(output, &signatures); err != nil || len(signatures) == 0 {
		return "", fmt.Errorf("%w: missing verified payload", ErrSignatureUnverified)
	}
	digest := signatures[0].Critical.Image.Digest
	for _, signature := range signatures {
		if !digestPattern.MatchString(digest) || signature.Critical.Image.Digest != digest {
			return "", fmt.Errorf("%w: invalid or conflicting verified digests", ErrSignatureUnverified)
		}
	}
	if _, requested, pinned := strings.Cut(image, "@"); pinned && requested != digest {
		return "", fmt.Errorf("%w: verified digest differs from requested pin", ErrSignatureUnverified)
	}
	return repository + "@" + digest, nil
}
