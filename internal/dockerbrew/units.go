package dockerbrew

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The units are moby's contrib/init/systemd/docker.service and docker.socket
// at moby/moby commit 4237bc6cb7f07343efa5b42b2f532050377a108b (master on
// 2026-10-26; both files last changed in 7263ae74cd8c80625a3dd04afe290ad1077b3bf9),
// https://raw.githubusercontent.com/moby/moby/4237bc6cb7f07343efa5b42b2f532050377a108b/contrib/init/systemd/docker.service
// and .../docker.socket. Only the ExecStart line differs, plus an
// Environment=PATH line: no containerd.service is installed, so dockerd runs
// its own managed containerd, and it finds containerd, the shim, runc,
// docker-init, and docker-proxy through that PATH.
const mobyServiceHead = `[Unit]
Description=Docker Application Container Engine
Documentation=https://docs.docker.com
After=network-online.target nss-lookup.target docker.socket firewalld.service containerd.service time-set.target
Wants=network-online.target containerd.service
Requires=docker.socket
StartLimitBurst=3
StartLimitIntervalSec=60

[Service]
Type=notify
# the default is not to use systemd for cgroups because the delegate issues still
# exists and systemd currently does not support the cgroup feature set required
# for containers run by docker
`

const mobyServiceTail = `ExecReload=/bin/kill -s HUP $MAINPID
TimeoutStartSec=0
RestartSec=2
Restart=always

# Having non-zero Limit*s causes performance problems due to accounting overhead
# in the kernel. We recommend using cgroups to do container-local accounting.
LimitNPROC=infinity
LimitCORE=infinity

# Comment TasksMax if your systemd version does not support it.
# Only systemd 226 and above support this option.
TasksMax=infinity

# set delegate yes so that systemd does not reset the cgroups of docker containers
Delegate=yes

# kill only the docker process, not all processes in the cgroup
KillMode=process
OOMScoreAdjust=-500

[Install]
WantedBy=multi-user.target
`

const mobySocket = `[Unit]
Description=Docker Socket for the API

[Socket]
# If /var/run is not implemented as a symlink to /run, you may need to
# specify ListenStream=/var/run/docker.sock instead.
ListenStream=/run/docker.sock
SocketMode=0660
SocketUser=root
SocketGroup=docker

[Install]
WantedBy=sockets.target
`

// ServiceUnit is moby's docker.service running the engine installed under root.
func ServiceUnit(root string) string {
	bin := root + "/current/bin"
	return ManagedMarker + "\n" + mobyServiceHead +
		"ExecStart=" + bin + "/dockerd -H fd://\n" +
		"Environment=PATH=" + bin + ":/usr/sbin:/usr/bin:/sbin:/bin\n" +
		mobyServiceTail
}

// SocketUnit is moby's docker.socket.
func SocketUnit() string {
	return ManagedMarker + "\n" + mobySocket
}

// ErrUnmanagedUnit reports a unit file ChairLift did not write.
var ErrUnmanagedUnit = errors.New("dockerbrew: refusing to replace a unit without the managed marker")

// CheckUnits refuses, with ErrUnmanagedUnit, a docker.service or
// docker.socket in dir that ChairLift did not write (or that is not a regular
// file), so callers can refuse before downloading anything.
func CheckUnits(dir string) error {
	_, err := managedUnits(dir)
	return err
}

// managedUnits returns the current contents of each managed unit name
// present in dir, refusing any unmanaged one.
func managedUnits(dir string) (map[string]string, error) {
	cur := map[string]string{}
	for _, name := range []string{ServiceName, SocketName} {
		body, managed, err := readUnit(filepath.Join(dir, name))
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return nil, err
		case !managed:
			return nil, fmt.Errorf("%w: %s", ErrUnmanagedUnit, filepath.Join(dir, name))
		default:
			cur[name] = body
		}
	}
	return cur, nil
}

// WriteUnits writes the managed service and socket units into dir. An
// existing file without ManagedMarker, or anything that is not a regular
// file, is refused.
func WriteUnits(dir, root string) (changed bool, err error) {
	units := []struct{ name, body string }{
		{ServiceName, ServiceUnit(root)},
		{SocketName, SocketUnit()},
	}
	// Check both before writing either, so a refusal leaves no half pair.
	cur, err := managedUnits(dir)
	if err != nil {
		return false, err
	}
	for _, u := range units {
		if body, ok := cur[u.name]; ok && body == u.body {
			continue
		}
		if err := writeFileAtomic(filepath.Join(dir, u.name), []byte(u.body), 0o644); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// RemoveManagedUnits deletes docker.service and docker.socket from dir only
// when they carry ManagedMarker.
func RemoveManagedUnits(dir string) (removed bool, err error) {
	for _, name := range []string{ServiceName, SocketName} {
		p := filepath.Join(dir, name)
		_, managed, err := readUnit(p)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrUnmanagedUnit) {
			continue
		}
		if err != nil {
			return removed, err
		}
		if !managed {
			continue
		}
		if err := os.Remove(p); err != nil {
			return removed, err
		}
		removed = true
	}
	return removed, nil
}

// readUnit returns a unit's contents and whether its first line is the
// marker. A symlink or other non-regular file is reported as unmanaged.
func readUnit(p string) (string, bool, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return "", false, err
	}
	if !fi.Mode().IsRegular() {
		return "", false, fmt.Errorf("%w: %s is not a regular file", ErrUnmanagedUnit, p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", false, err
	}
	first, _, _ := strings.Cut(string(data), "\n")
	return string(data), strings.TrimRight(string(first), "\r") == ManagedMarker, nil
}

func writeFileAtomic(p string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
