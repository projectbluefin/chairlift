#!/usr/bin/env bash
# Provision only the disposable rootless Podman systemd container. No host state.
# The pinned Dakota image is used unchanged: no Docker package is baked in.
set -euo pipefail
UID_NUMBER="${1:?fixture uid required}"
GID_NUMBER="${2:?fixture gid required}"
[ "$(id -u)" = 0 ] || { echo "fixture setup requires container root" >&2; exit 1; }
[ "$(cat /proc/1/comm)" = systemd ] || { echo "fixture requires native systemd PID 1" >&2; exit 1; }
for _ in {1..300}; do
    [ -S /run/systemd/private ] && break
    sleep 0.1
done
[ -S /run/systemd/private ] || { echo "fixture systemd manager did not become ready" >&2; exit 1; }
ROOT="${3:?checkout path required}"
if [ "${4:-}" = verify ]; then
    # The test account cannot traverse Docker's root-owned data directory.
    # Check it here after the real GUI has stopped both managed system units.
    test -d /var/lib/docker/volumes/chairlift-system-integration/_data
    test -s /var/lib/docker/volumes/metadata.db
    for unit in docker.socket docker.service; do
        [ "$(systemctl show "$unit" --property=LoadState --value)" = loaded ]
        [ "$(systemctl show "$unit" --property=ActiveState --value)" = inactive ]
        [ "$(systemctl show "$unit" --property=FragmentPath --value)" = "/etc/systemd/system/$unit" ]
        [ "$(head -n1 "/etc/systemd/system/$unit")" = '# Managed by chairlift-helper' ]
    done
    getent group docker | cut -d: -f4 | tr , '\n' | grep -qxF "$(getent passwd "$UID_NUMBER" | cut -d: -f1)"
    # Everything the helper installed stays root-owned and is retained, under
    # ChairLift's /var state directory; nothing is written through /usr/local.
    test -s /var/lib/chairlift/docker/current/manifest.json
    [ -z "$(find -P /var/lib/chairlift/ \( ! -user 0 -o ! -group 0 -o -perm /022 ! -type l \) -print)" ]
    [ ! -e "$(readlink -m /usr/local)/libexec/docker" ]
    echo 'LIVE DOCKER DATA PROOF: managed units stopped; Homebrew runtime, docker group membership and daemon-managed volume retained'
    exit 0
fi
# Boot only the native services this fixture needs, not image preinstall jobs.
timeout 90 systemctl start basic.target dbus.socket
systemctl is-active --quiet basic.target dbus.socket
# Native boot can mount /home after Podman's initial bind mounts. Keep the
# checkout reachable at its original absolute path through a stable /opt bind.
# ROOT is the checkout mounted by the enclosing rootless Podman harness.
if [ ! -e "$ROOT" ]; then
    mkdir -p "$(dirname "$ROOT")"
    ln -s /opt/chairlift-checkout "$ROOT"
fi
setpriv --reuid="$UID_NUMBER" --regid="$GID_NUMBER" --clear-groups test -r "$ROOT/go.mod" || {
    stat "$ROOT" "$(dirname "$ROOT")" /opt/chairlift-checkout >&2
    echo "fixture checkout is not readable by its test account" >&2
    exit 1
}
ACCOUNT="$(getent passwd "$UID_NUMBER" | cut -d: -f1)"
[ -n "$ACCOUNT" ] || { echo "fixture account is missing" >&2; exit 1; }

# The image ships no Docker: the real helper installs it from verified
# Homebrew bottles, so this fixture provides only what the daemon and the
# driver need from the base image, and reaches the network for the bottles.
for command in iptables ip6tables nft ip curl sg groupadd; do
    command -v "$command"
done
for command in dockerd docker containerd runc; do
    if command -v "$command"; then
        echo "fixture image unexpectedly ships $command" >&2
        exit 1
    fi
done
[ ! -e /usr/lib/systemd/system/docker.service ] || { echo "fixture must exercise the Homebrew install branch, not image units" >&2; exit 1; }
[ ! -e /var/lib/chairlift ] || { echo "fixture already has a Docker runtime" >&2; exit 1; }
for unit in docker.socket docker.service; do
    [ "$(systemctl show "$unit" --property=LoadState --value)" = not-found ]
done
# Start without a docker group, as a fresh Dakota install does: the helper
# must create it. An image-only copy in /usr/lib/group would defeat that.
if grep -q '^docker:' /etc/group; then
    groupdel docker
fi
if getent group docker; then
    echo "fixture image defines a docker group the helper cannot be shown to create" >&2
    exit 1
fi
# A disposable password authenticates through real PolicyKit, never a rule
# granting passwordless access. All account changes live in this container.
command -v pkttyagent
command -v chpasswd
getent group wheel || groupadd wheel
usermod -aG wheel "$ACCOUNT"
printf '%s:%s\n' "$ACCOUNT" 'chairlift-private-fixture-only' | chpasswd
for group in incus-admin libvirt dialout; do
    getent group "$group" || groupadd "$group"
    gpasswd -d "$ACCOUNT" "$group" || true
done
# Install this checkout's real helper and matching policies, not an image copy.
install -m755 "${CHAIRLIFT_E2E_BUILD_DIR:-$ROOT/build}/chairlift-helper" /usr/bin/chairlift-helper
install -m644 "$ROOT/data/io.projectbluefin.chairlift.ublue.policy" /usr/share/polkit-1/actions/io.projectbluefin.chairlift.ublue.policy
systemctl start polkit.service

mkdir -p /tmp/home
chown "$UID_NUMBER:$GID_NUMBER" /tmp/home
usermod --home /tmp/home "$ACCOUNT"
systemctl start "user@$UID_NUMBER.service" || {
    journalctl -b -u "user@$UID_NUMBER.service" --no-pager >&2
    exit 1
}
# The target is this container's private manager, never the host's /run/user.
# The compositor wrapper deliberately refuses literal /run/user runtime paths.
ln -s "/run/user/$UID_NUMBER" /tmp/chairlift-docker-runtime
setpriv --reuid="$UID_NUMBER" --regid="$GID_NUMBER" --clear-groups \
    env HOME=/tmp/home XDG_RUNTIME_DIR=/tmp/chairlift-docker-runtime \
    systemctl --user show-environment

# Reproduce what v26.10.10 left behind: a rootless Docker user unit started by
# dockerd-rootless-setuptool.sh. Its stand-in daemon only sleeps, so the live
# gate can observe ChairLift stopping and disabling it before system Docker.
install -d -o "$UID_NUMBER" -g "$GID_NUMBER" -m 0755 \
    /tmp/home/.local /tmp/home/.local/bin /tmp/home/.config /tmp/home/.config/systemd /tmp/home/.config/systemd/user
printf '%s\n' '#!/bin/sh' 'exec sleep infinity' > /tmp/home/.local/bin/dockerd-rootless.sh
cat > /tmp/home/.config/systemd/user/docker.service <<'UNIT'
[Unit]
Description=Docker Application Container Engine (Rootless)

[Service]
ExecStart=/tmp/home/.local/bin/dockerd-rootless.sh
Type=simple

[Install]
WantedBy=default.target
UNIT
chown "$UID_NUMBER:$GID_NUMBER" /tmp/home/.local/bin/dockerd-rootless.sh /tmp/home/.config/systemd/user/docker.service
chmod 0755 /tmp/home/.local/bin/dockerd-rootless.sh
chmod 0644 /tmp/home/.config/systemd/user/docker.service
as_user() {
    setpriv --reuid="$UID_NUMBER" --regid="$GID_NUMBER" --clear-groups \
        env HOME=/tmp/home XDG_RUNTIME_DIR=/tmp/chairlift-docker-runtime "$@"
}
as_user systemctl --user daemon-reload
as_user systemctl --user enable --now docker.service
[ "$(as_user systemctl --user is-active docker.service)" = active ]
