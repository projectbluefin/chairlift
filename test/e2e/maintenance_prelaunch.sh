#!/usr/bin/env bash
# Pre-launch hook for Maintenance AT-SPI scenario:
# sets up private stub binaries for flatpak and brew, and writes config.dev.yml
# enabling maintenance_freespace_group and reset_group beside the executable.
set -euo pipefail

OUTDIR="${CHAIRLIFT_ATSPI_OUTDIR:-$OUTDIR}"

BINDIR="$OUTDIR/bin"
mkdir -p "$BINDIR"

cat << 'EOF' > "$BINDIR/flatpak"
#!/bin/sh
case "${1:-}" in
    --version)
        echo "Flatpak 1.14.4"
        exit 0
        ;;
    uninstall)
        echo "Nothing unused to uninstall"
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
EOF
chmod +x "$BINDIR/flatpak"

cat << 'EOF' > "$BINDIR/brew"
#!/bin/sh
case "${1:-}" in
    --version)
        echo "Homebrew 4.2.0"
        exit 0
        ;;
    cleanup)
        # In dry-run/test mode, pause briefly if probe release marker requested or sleep briefly
        # to ensure busy state is observable.
        sleep 0.8
        echo "Homebrew cache cleaned"
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
EOF
chmod +x "$BINDIR/brew"

export PATH="$BINDIR:$PATH"

# Write config.dev.yml beside the app binary or in cwd so reset_group is enabled
cat << 'EOF' > "$OUTDIR/config.dev.yml"
maintenance_page:
  maintenance_freespace_group:
    enabled: true
  reset_group:
    enabled: true
EOF
# Config is loaded from cwd () where runner executes
