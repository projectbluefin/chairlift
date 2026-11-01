package dockerbrew

import (
	"archive/tar"
	"compress/gzip"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// bottleFiles maps each formula's bottle-relative member to its installed
// path. Nothing else in a bottle is extracted.
var bottleFiles = map[string]map[string]string{
	"docker-engine": {
		"bin/dockerd":      "bin/dockerd",
		"bin/docker-proxy": "bin/docker-proxy",
	},
	"containerd": {
		"bin/containerd":              "bin/containerd",
		"bin/containerd-shim-runc-v2": "bin/containerd-shim-runc-v2",
	},
	"runc": {
		"sbin/runc": "bin/runc",
	},
	// dockerd runs docker-init from PATH; in the bottle bin/docker-init is a
	// symlink to tini-static, and symlinks are never followed.
	"tini": {
		"bin/tini-static": "bin/docker-init",
	},
}

// bottleLibs maps library formulae to the one soname each provides. The
// bottle's soname entry is a symlink to a versioned file whose name does not
// follow the formula version (libpathrs 0.2.6 ships libpathrs.so.0.2.5+dev),
// so the single regular file under lib/ named "<soname>" or "<soname>.*" is
// installed as lib/<soname> after its DT_SONAME is checked.
var bottleLibs = map[string]string{
	"libpathrs":  "libpathrs.so.0",
	"libseccomp": "libseccomp.so.2",
}

// Extract unpacks the mapped members of a gzip-compressed bottle into dest.
// Members are taken only as regular files; symlinks and hard links are never
// followed or created. Absolute names, ".." components, and device or FIFO
// members fail the whole bottle.
func Extract(r io.Reader, b Bottle, dest string) error {
	files, isBin := bottleFiles[b.Name]
	soname, isLib := bottleLibs[b.Name]
	if !isBin && !isLib {
		return fmt.Errorf("dockerbrew: %q is not in the fixed formula set", b.Name)
	}
	if !versionPattern.MatchString(b.Version) || strings.Contains(b.Version, "..") || b.Revision < 0 {
		return fmt.Errorf("dockerbrew: %s has unusable version %q", b.Name, b.Dir())
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("dockerbrew: %s bottle: %w", b.Name, err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	prefix := b.Name + "/" + b.Dir() + "/"
	found := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("dockerbrew: %s bottle: %w", b.Name, err)
		}
		name := hdr.Name
		if strings.HasPrefix(name, "/") || slices.Contains(strings.Split(name, "/"), "..") {
			return fmt.Errorf("dockerbrew: %s bottle member %q escapes the bottle", b.Name, name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir, tar.TypeSymlink, tar.TypeLink:
			continue
		case tar.TypeReg:
		default:
			return fmt.Errorf("dockerbrew: %s bottle member %q has unsupported type %q", b.Name, name, hdr.Typeflag)
		}
		rel, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		rel = path.Clean(rel)
		var dst string
		var mode os.FileMode
		if isBin {
			if dst = files[rel]; dst == "" {
				continue
			}
			mode = 0o755
		} else {
			dir, base := path.Split(rel)
			if dir != "lib/" || (base != soname && !strings.HasPrefix(base, soname+".")) {
				continue
			}
			dst, mode = "lib/"+soname, 0o644
		}
		if found[dst] {
			return fmt.Errorf("dockerbrew: %s bottle has more than one regular file for %s", b.Name, dst)
		}
		found[dst] = true
		if err := writeMember(tr, filepath.Join(dest, filepath.FromSlash(dst)), mode); err != nil {
			return fmt.Errorf("dockerbrew: %s bottle: %w", b.Name, err)
		}
	}
	if isBin {
		for src, dst := range files {
			if !found[dst] {
				return fmt.Errorf("dockerbrew: %s bottle has no regular file %s%s", b.Name, prefix, src)
			}
		}
		return nil
	}
	dst := "lib/" + soname
	if !found[dst] {
		return fmt.Errorf("dockerbrew: %s bottle has no regular file for %s", b.Name, soname)
	}
	return checkSoname(filepath.Join(dest, dst), soname)
}

func writeMember(r io.Reader, target string, mode os.FileMode) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func checkSoname(file, soname string) error {
	f, err := elf.Open(file)
	if err != nil {
		return fmt.Errorf("dockerbrew: %s: %w", soname, err)
	}
	defer func() { _ = f.Close() }()
	names, err := f.DynString(elf.DT_SONAME)
	if err != nil {
		return fmt.Errorf("dockerbrew: %s: %w", soname, err)
	}
	if len(names) != 1 || names[0] != soname {
		return fmt.Errorf("dockerbrew: library installed as %s has DT_SONAME %v", soname, names)
	}
	return nil
}
