package aistack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// defaultPeerPort is llmman's default listen port, used when a peer address
// carries none.
const defaultPeerPort = "17434"

// peersFileName is ChairLift's own bookkeeping file — never llmman's TOML.
// It records only addresses and whether each is currently offered to
// llmman's aggregation.peers, so a disabled peer can be re-enabled without
// retyping it. No credential is ever written here: the one client key used
// to authenticate to peers lives only in llmman's own configuration,
// reachable through `llmman config get/set aggregation.api_key`, and is
// never read back, logged, or displayed by ChairLift after entry.
const peersFileName = "agent-mode-peers.json"

// Peer is one configured offload target: an already-configured llmman
// service on another machine that this host may route requests to. Adding a
// peer never makes this host reachable from anywhere; it only teaches this
// llmman about another one to ask.
type Peer struct {
	// Address is the normalized [scheme://]host[:port] form ParsePeerAddress
	// produced.
	Address string `json:"address"`
	// Enabled reports whether Address is currently included in the value
	// applied to llmman's aggregation.peers. Disabling keeps the entry in
	// ChairLift's list without deleting it or touching llmman's
	// configuration for it beyond removing it from that one value.
	Enabled bool `json:"enabled"`
}

// PeerStatus is one point-in-time read of a peer's /llmman/node endpoint.
//
// A timeout or connection failure is not the same as an empty answer:
// Reachable stays false and Loaded/Stored stay nil, so a caller can tell
// "could not ask" from "asked, and it has nothing loaded" rather than
// silently treating the former as the latter.
type PeerStatus struct {
	Address      string
	Reachable    bool
	Unauthorized bool
	Memory       int64
	Loaded       map[string]int64
	Stored       map[string]int64
	// Error is a short, human-facing reason, populated whenever Reachable
	// is false or Unauthorized is true. It never contains the API key: the
	// key travels only in a request header this package builds itself, and
	// is never interpolated into an error or log line anywhere below.
	Error string
}

// peerAddressRE matches a bare hostname per RFC 1123's relaxed label rules.
// IP literals are validated separately with net.ParseIP.
var peerAddressRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,62})?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,62})?)*$`)

// ParsePeerAddress validates raw against llmman's documented peer grammar,
// `[scheme://]host[:port]`, and returns it in the normalized form ChairLift
// stores and hands to `llmman config set aggregation.peers`.
//
// Only "http" and "https" are accepted schemes; anything else (including no
// scheme at all being fine, but a scheme like "ftp" or "ssh" not) is
// rejected. Embedded credentials ("user:pass@host") and any path, query, or
// fragment are rejected outright — this grammar names an endpoint, not a
// URL to fetch.
func ParsePeerAddress(raw string) (string, error) {
	scheme, host, port, err := parsePeerParts(raw)
	if err != nil {
		return "", err
	}
	normalized := host
	if !isIPLiteral(host) {
		normalized = strings.ToLower(host)
	} else if strings.Contains(host, ":") {
		// An IPv6 literal must stay bracketed once a port is appended, and
		// ChairLift's own stored form always brackets it so RemovePeer and
		// SetPeerEnabled can match it back against a freshly parsed input.
		normalized = "[" + host + "]"
	}
	if port != "" {
		normalized += ":" + port
	}
	if scheme != "" {
		normalized = scheme + "://" + normalized
	}
	return normalized, nil
}

// parsePeerParts is the shared grammar check behind ParsePeerAddress and
// NodeURL, so both apply exactly the same rules to exactly the same input.
func parsePeerParts(raw string) (scheme, host, port string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", "", errors.New("peer address is empty")
	}

	rest := trimmed
	if i := strings.Index(trimmed, "://"); i >= 0 {
		scheme = strings.ToLower(trimmed[:i])
		rest = trimmed[i+3:]
		if scheme != "http" && scheme != "https" {
			return "", "", "", fmt.Errorf("peer address %q uses unsupported scheme %q; only http and https are accepted", raw, scheme)
		}
	}
	if rest == "" {
		return "", "", "", fmt.Errorf("peer address %q has no host", raw)
	}
	if strings.Contains(rest, "@") {
		return "", "", "", fmt.Errorf("peer address %q must not embed credentials", raw)
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		return "", "", "", fmt.Errorf("peer address %q must be host[:port] only, not a URL with a path", raw)
	}

	host, port, err = splitPeerHostPort(rest)
	if err != nil {
		return "", "", "", fmt.Errorf("peer address %q: %w", raw, err)
	}
	if !validPeerHost(host) {
		return "", "", "", fmt.Errorf("peer address %q has an invalid host %q", raw, host)
	}
	if port != "" {
		n, convErr := strconv.Atoi(port)
		if convErr != nil || n < 1 || n > 65535 {
			return "", "", "", fmt.Errorf("peer address %q has an invalid port %q", raw, port)
		}
	}
	return scheme, host, port, nil
}

// splitPeerHostPort splits rest into host and an optional port, accepting a
// bracketed IPv6 literal ("[::1]:17434" or "[::1]"), a bare IPv6 literal
// with no port ("::1", which cannot carry a port without brackets), or a
// plain host[:port].
func splitPeerHostPort(rest string) (host, port string, err error) {
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return "", "", errors.New("unterminated IPv6 bracket")
		}
		host = rest[1:end]
		if net.ParseIP(host) == nil {
			return "", "", fmt.Errorf("invalid IPv6 address %q", host)
		}
		remainder := rest[end+1:]
		if remainder == "" {
			return host, "", nil
		}
		if !strings.HasPrefix(remainder, ":") {
			return "", "", fmt.Errorf("unexpected characters after IPv6 address: %q", remainder)
		}
		return host, remainder[1:], nil
	}

	if strings.Count(rest, ":") >= 2 {
		// A bare literal with two or more colons and no brackets can only
		// be IPv6, and IPv6 needs brackets to carry a port.
		if net.ParseIP(rest) == nil {
			return "", "", fmt.Errorf("IPv6 address %q must be bracketed (e.g. [%s]:17434) to add a port", rest, rest)
		}
		return rest, "", nil
	}

	if i := strings.LastIndex(rest, ":"); i >= 0 {
		host, port = rest[:i], rest[i+1:]
		if host == "" {
			return "", "", errors.New("no host before ':'")
		}
		return host, port, nil
	}
	return rest, "", nil
}

func isIPLiteral(host string) bool {
	return net.ParseIP(host) != nil
}

func validPeerHost(host string) bool {
	if host == "" {
		return false
	}
	if isIPLiteral(host) {
		return true
	}
	return peerAddressRE.MatchString(host)
}

// NodeURL returns the full URL of a peer's status endpoint. It applies the
// same grammar as ParsePeerAddress and defaults to llmman's own default
// port when address carries none.
func NodeURL(address string) (string, error) {
	scheme, host, port, err := parsePeerParts(address)
	if err != nil {
		return "", err
	}
	if scheme == "" {
		scheme = "http"
	}
	if port == "" {
		port = defaultPeerPort
	}
	hostport := host
	if isIPLiteral(host) && strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	return fmt.Sprintf("%s://%s:%s/llmman/node", scheme, hostport, port), nil
}

// peerProbeTimeout bounds one status request. A peer that does not answer
// within it is reported unreachable, never as having no models.
var peerProbeTimeout = 3 * time.Second

// httpClient is the seam ProbePeer sends requests through; replaced in
// tests that need to force a timeout independent of a real network.
var httpClient = http.DefaultClient

// ProbePeer performs one bounded GET of address's /llmman/node, carrying
// apiKey as a bearer credential when set, and reports what it learned.
//
// apiKey is never logged, never placed in PeerStatus.Error, and is sent
// only in the Authorization header of this one request.
func ProbePeer(ctx context.Context, address, apiKey string) PeerStatus {
	status := PeerStatus{Address: address}

	url, err := NodeURL(address)
	if err != nil {
		status.Error = "this address is not valid"
		return status
	}

	ctx, cancel := context.WithTimeout(ctx, peerProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		status.Error = "could not build the status request"
		return status
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Unreachable — a timed-out or refused connection is not evidence
		// the peer has nothing; it is evidence nothing could be asked.
		status.Error = "not reachable"
		return status
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		status.Unauthorized = true
		status.Error = "the peer rejected the configured key"
		return status
	}
	if resp.StatusCode != http.StatusOK {
		status.Error = fmt.Sprintf("unexpected response (status %d)", resp.StatusCode)
		return status
	}

	var node struct {
		Memory int64            `json:"memory"`
		Loaded map[string]int64 `json:"loaded"`
		Stored map[string]int64 `json:"stored"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		status.Error = "the response could not be understood"
		return status
	}

	status.Reachable = true
	status.Memory = node.Memory
	status.Loaded = node.Loaded
	status.Stored = node.Stored
	return status
}

// Seams for the peer store's location and llmman's config commands, swapped
// in tests.
var (
	homeDir            = os.UserHomeDir
	runConfigSet       = defaultRunConfigSet
	runSecretConfigSet = defaultRunSecretConfigSet
)

func defaultRunConfigSet(ctx context.Context, exe, key, value string) error {
	_, err := run(ctx, exe, "config", "set", key, value)
	return err
}

// defaultRunSecretConfigSet sets a credential-carrying key. It never joins
// value into the returned error — only defaultRunConfigSet's non-secret
// callers may do that — so a rejected key cannot end up in a log line.
func defaultRunSecretConfigSet(ctx context.Context, exe, key, value string) error {
	if err := exec.CommandContext(ctx, exe, "config", "set", key, value).Run(); err != nil {
		return fmt.Errorf("llmman config set %s: %w", key, err)
	}
	return nil
}

func peersStorePath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "chairlift", peersFileName), nil
}

type peerStore struct {
	Peers []Peer `json:"peers"`
}

func loadPeerStore() (peerStore, error) {
	path, err := peersStorePath()
	if err != nil {
		return peerStore{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return peerStore{}, nil
		}
		return peerStore{}, err
	}
	var s peerStore
	if err := json.Unmarshal(data, &s); err != nil {
		return peerStore{}, fmt.Errorf("peer list %q is corrupt: %w", path, err)
	}
	return s, nil
}

func savePeerStore(s peerStore) error {
	path, err := peersStorePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, string(data)+"\n")
}

// Peers returns every configured peer, enabled or not. An empty, never-used
// list returns an empty slice and a nil error.
func Peers() ([]Peer, error) {
	s, err := loadPeerStore()
	if err != nil {
		return nil, err
	}
	return s.Peers, nil
}

// applyPeerList pushes the enabled subset of s.Peers to llmman as the single
// aggregation.peers value. An empty result clears it. This is the only
// place ChairLift's peer list and llmman's own configuration are
// reconciled; every mutator below calls it before persisting its own state,
// so a config-command failure leaves neither side changed.
func applyPeerList(ctx context.Context, s peerStore) error {
	exe := Executable()
	if exe == "" {
		return errors.New("llmman is not installed")
	}
	var enabled []string
	for _, p := range s.Peers {
		if p.Enabled {
			enabled = append(enabled, p.Address)
		}
	}
	if err := runConfigSet(ctx, exe, "aggregation.peers", strings.Join(enabled, ",")); err != nil {
		return fmt.Errorf("configuring peers with llmman: %w", err)
	}
	return nil
}

// AddPeer validates address, rejects a duplicate of an already-configured
// peer, and adds it enabled. The new list is applied to llmman before it is
// persisted; a failure there leaves both unchanged.
func AddPeer(ctx context.Context, address string) error {
	normalized, err := ParsePeerAddress(address)
	if err != nil {
		return err
	}
	s, err := loadPeerStore()
	if err != nil {
		return err
	}
	for _, p := range s.Peers {
		if p.Address == normalized {
			return fmt.Errorf("%s is already configured", normalized)
		}
	}
	next := peerStore{Peers: append(append([]Peer{}, s.Peers...), Peer{Address: normalized, Enabled: true})}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would configure llmman peers %v and add %s to the peer store", peerAddresses(next), normalized)
		return nil
	}
	if err := applyPeerList(ctx, next); err != nil {
		return err
	}
	return savePeerStore(next)
}

// peerAddresses is a small dry-run logging helper.
func peerAddresses(s peerStore) []string {
	addrs := make([]string, 0, len(s.Peers))
	for _, p := range s.Peers {
		addrs = append(addrs, p.Address)
	}
	return addrs
}

// RemovePeer deletes a configured peer entirely. Applied to llmman before
// it is persisted, like AddPeer.
func RemovePeer(ctx context.Context, address string) error {
	normalized, err := ParsePeerAddress(address)
	if err != nil {
		return err
	}
	s, err := loadPeerStore()
	if err != nil {
		return err
	}
	idx := indexOfPeer(s.Peers, normalized)
	if idx < 0 {
		return fmt.Errorf("%s is not configured", normalized)
	}
	updated := append(append([]Peer{}, s.Peers[:idx]...), s.Peers[idx+1:]...)
	next := peerStore{Peers: updated}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would configure llmman peers %v and remove %s from the peer store", peerAddresses(next), normalized)
		return nil
	}
	if err := applyPeerList(ctx, next); err != nil {
		return err
	}
	return savePeerStore(next)
}

// SetPeerEnabled turns one configured peer on or off without forgetting it.
// A disabled peer is removed from the value handed to llmman but stays in
// ChairLift's own list so it can be turned back on later.
func SetPeerEnabled(ctx context.Context, address string, enabled bool) error {
	normalized, err := ParsePeerAddress(address)
	if err != nil {
		return err
	}
	s, err := loadPeerStore()
	if err != nil {
		return err
	}
	idx := indexOfPeer(s.Peers, normalized)
	if idx < 0 {
		return fmt.Errorf("%s is not configured", normalized)
	}
	updated := append([]Peer{}, s.Peers...)
	updated[idx].Enabled = enabled
	next := peerStore{Peers: updated}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would configure llmman peers %v (setting %s enabled=%v) and persist the peer store", peerAddresses(next), normalized, enabled)
		return nil
	}
	if err := applyPeerList(ctx, next); err != nil {
		return err
	}
	return savePeerStore(next)
}

func indexOfPeer(peers []Peer, address string) int {
	for i, p := range peers {
		if p.Address == address {
			return i
		}
	}
	return -1
}

// SetPeerAPIKey stores the one client credential ChairLift sends when it
// connects out to an authenticated peer, through llmman's own
// `config set`, which validates the key, preserves the file layout, and
// enforces secure permissions. The key is never read back, logged, or
// shown by ChairLift after this call returns.
func SetPeerAPIKey(ctx context.Context, apiKey string) error {
	exe := Executable()
	if exe == "" {
		return errors.New("llmman is not installed")
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set llmman aggregation.api_key via %s config set", exe)
		return nil
	}
	return runSecretConfigSet(ctx, exe, "aggregation.api_key", apiKey)
}
