# Spec: Dinosaur Avatars & User Account Integration

This specification governs the discovery, retrieval, transformation, and application
of dinosaur user avatars from `projectbluefin/website` within Control Center.

## Interface

### Catalog Manifest (`assets/dino-avatars.txt`)

An embedded tab-separated manifest in `internal/avatar` containing:

| Field | Format | Example |
| --- | --- | --- |
| `ID` | Lowercase alphanumeric slug | `bluefin` |
| `Common Name` | Human-readable name | `Bluefin (Deinonychus)` |
| `Species` | Scientific taxonomic name | `Deinonychus antirrhopus` |
| `Path` | Exact repository path | `public/characters/bluefin.webp` |

### URL Resolution and Pinned Ref

Remote assets resolve against the raw GitHub URL pinned to a specific commit:

```
https://raw.githubusercontent.com/projectbluefin/website/92b81281649c0795f16f266f712c9a2798af54c4/<Path>
```

Tests point `Fetch` at a local `httptest.Server` by stripping the base URL prefix.

### Seams and Signatures

```go
package avatar

// FetchFunc represents the network fetch seam for remote avatar assets.
type FetchFunc func(ctx context.Context, url string) ([]byte, error)

// ApplierFunc represents the system dispatch seam for setting the user icon.
type ApplierFunc func(ctx context.Context, uid int, pngPath string) error
```

### Application CLI Invocation

The default `ApplierFunc` dispatches to AccountsService via `busctl`:
```bash
busctl call org.freedesktop.Accounts \
  /org/freedesktop/Accounts/User<UID> \
  org.freedesktop.Accounts.User SetIconFile s "<PNG_PATH>"
```
Only if this D-Bus call returns an error or AccountsService is unreachable does
the applier fall back to copying the PNG file to `~/.face.icon` and `~/.face`.

## Rules

1. **Manifest Paths Are Literal:** Artwork paths are never derived from IDs or slugs.
   Every entry resolves strictly via the `Path` column in `dino-avatars.txt` to prevent
   404 errors caused by legacy uppercase prefixes or species typos.
2. **Mandatory In-Memory Transcoding & Size Bound:** All fetched WebP assets must be
   decoded in pure Go via `golang.org/x/image/webp`, resized to 512×512, and encoded to
   PNG via `image/png`. The resulting PNG must not exceed 1 MiB (1,048,576 bytes), which
   is AccountsService's hard icon size ceiling (`user_change_icon_file_authorized_cb`).
   If an encoded PNG exceeds 1 MiB, it must be downsampled (or step down dimensions)
   prior to dispatch, returning an explicit `ErrAvatarTooLarge` if reduction fails.
3. **No Root Privilege:** Avatar updates must execute as the current unprivileged user.
   Privileged helpers (`chairlift-ublue-helper`) must not be invoked.
4. **Offline Resilience:** If network fetch fails, the UI displays an inline banner; it
   must not crash, hang, or leave partial temporary files on disk.
5. **Dry-Run Gating:** Under `--dry-run`, `[DRY-RUN] would set avatar to <ID>` is logged,
   and no D-Bus calls or file modifications to `~/.face` are executed.

## References

- Rationale: [ADR-0015](../adr/0015-avatars-convert-to-png-before-accountsservice-seticonfile.md)
- Context: [design/overview.md](../design/overview.md)
