#!/usr/bin/env bash
# Pre-launch hook for Agent Mode preset AT-SPI scenario:
# sets up isolated Homebrew/llmman stubs, systemd unit, proxy blocker,
# and background node stub.
OUTDIR="${CHAIRLIFT_ATSPI_OUTDIR:-$OUTDIR}"

BIN="$OUTDIR/bin"
RUNTIME="$OUTDIR/runtime"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}"
mkdir -p "$CONFIG_DIR/systemd/user" "$BIN" "$RUNTIME"
chmod 0700 "$RUNTIME"

cat > "$BIN/brew" <<STUB
#!/bin/sh
echo "\$@" >> "$OUTDIR/brew_invocations.log"
exit 0
STUB
cat > "$BIN/llmman" <<STUB
#!/bin/sh
echo "\$@" >> "$OUTDIR/llmman_invocations.log"
exit 0
STUB
chmod 0755 "$BIN/brew" "$BIN/llmman"

cat > "$CONFIG_DIR/systemd/user/chairlift-llmman.service" <<'UNIT'
[Unit]
Description=Test-only Agent Mode unit
[Service]
ExecStart=/bin/true
UNIT

export PATH="$BIN:$PATH"
export HTTPS_PROXY=http://127.0.0.1:17435 https_proxy=http://127.0.0.1:17435
export NO_PROXY=127.0.0.1,localhost no_proxy=127.0.0.1,localhost
export CHAIRLIFT_AGENT_PROXY_LOG="$OUTDIR/proxy_blocked.log"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NODE_STUB="$SCRIPT_DIR/agent_presets_node_stub.py"
python3 "$NODE_STUB" >"$OUTDIR/node.log" 2>&1 &
PRELAUNCH_PID=$!

# Wait for node status and proxy endpoints to bind
ready=0
for _ in $(seq 1 100); do
    if ! kill -0 "$PRELAUNCH_PID" 2>/dev/null; then
        echo "Agent Mode node stub exited before binding:" >&2
        cat "$OUTDIR/node.log" >&2
        exit 1
    fi
    if python3 -c "import socket; s1=socket.create_connection(('127.0.0.1', 17434), timeout=.2); s1.close(); s2=socket.create_connection(('127.0.0.1', 17435), timeout=.2); s2.close()" 2>/dev/null; then
        ready=1
        break
    fi
    sleep 0.1
done
[ "$ready" = 1 ] || { echo "Agent Mode node stub did not start" >&2; cat "$OUTDIR/node.log" >&2; exit 1; }
