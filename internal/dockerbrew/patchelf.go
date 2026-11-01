package dockerbrew

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// originLib is the run path every patched file gets: the installed set's
// own lib directory, never a Homebrew prefix.
const originLib = "$ORIGIN/../lib"

// homebrewPlaceholder starts every path Homebrew leaves for relocation.
const homebrewPlaceholder = "@@HOMEBREW"

// PatchELF rewrites a bottle file in place so it loads nothing from a
// Homebrew prefix: a PT_INTERP that is a Homebrew placeholder becomes interp,
// and every DT_RPATH and DT_RUNPATH becomes "$ORIGIN/../lib". A replacement
// must fit the existing slot. Files with neither (static executables) are
// left untouched. Only little-endian ELF64 is accepted. Every check runs
// before the first write, so a refused file is left exactly as it was.
func PatchELF(path, interp string) (changed bool, err error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	ef, err := elf.NewFile(f)
	if err != nil {
		return false, fmt.Errorf("dockerbrew: %s: %w", path, err)
	}
	if ef.Class != elf.ELFCLASS64 || ef.Data != elf.ELFDATA2LSB {
		return false, fmt.Errorf("dockerbrew: %s is not a little-endian ELF64 file", path)
	}

	var writes []elfWrite
	for _, p := range ef.Progs {
		if p.Type != elf.PT_INTERP {
			continue
		}
		slot := make([]byte, p.Filesz)
		if _, err := f.ReadAt(slot, int64(p.Off)); err != nil {
			return false, fmt.Errorf("dockerbrew: %s: read PT_INTERP: %w", path, err)
		}
		cur := cstring(slot)
		switch {
		case cur == interp:
		case strings.HasPrefix(cur, homebrewPlaceholder):
			if interp == "" || len(interp)+1 > len(slot) {
				return false, fmt.Errorf("dockerbrew: %s: interpreter %q does not fit the %d-byte PT_INTERP slot", path, interp, len(slot))
			}
			writes = append(writes, elfWrite{int64(p.Off), padded(interp, len(slot))})
		default:
			return false, fmt.Errorf("dockerbrew: %s: unexpected interpreter %q", path, cur)
		}
	}
	dynWrites, err := planDynamic(f, ef, path)
	if err != nil {
		return false, err
	}
	for _, w := range append(writes, dynWrites...) {
		if _, err := f.WriteAt(w.data, w.off); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

type elfWrite struct {
	off  int64
	data []byte
}

// planDynamic validates the dynamic section and returns the run-path
// rewrites it needs.
func planDynamic(f *os.File, ef *elf.File, path string) ([]elfWrite, error) {
	var dyn *elf.Prog
	for _, p := range ef.Progs {
		if p.Type == elf.PT_DYNAMIC {
			dyn = p
			break
		}
	}
	if dyn == nil {
		return nil, nil
	}
	raw := make([]byte, dyn.Filesz)
	if _, err := f.ReadAt(raw, int64(dyn.Off)); err != nil {
		return nil, fmt.Errorf("dockerbrew: %s: read PT_DYNAMIC: %w", path, err)
	}
	var strtab, strsz uint64
	var paths, needed, others []uint64
	le := binary.LittleEndian
	for i := 0; i+16 <= len(raw); i += 16 {
		tag, val := elf.DynTag(le.Uint64(raw[i:])), le.Uint64(raw[i+8:])
		switch tag {
		case elf.DT_NULL:
			i = len(raw)
		case elf.DT_STRTAB:
			strtab = val
		case elf.DT_STRSZ:
			strsz = val
		case elf.DT_RPATH, elf.DT_RUNPATH:
			paths = append(paths, val)
		case elf.DT_NEEDED:
			needed = append(needed, val)
		case elf.DT_SONAME:
			others = append(others, val)
		}
	}
	if len(paths) == 0 && len(needed) == 0 {
		return nil, nil
	}
	strOff, ok := fileOffset(ef, strtab)
	if !ok || strsz == 0 {
		return nil, fmt.Errorf("dockerbrew: %s: dynamic string table is not in a loaded segment", path)
	}
	table := make([]byte, strsz)
	if _, err := f.ReadAt(table, int64(strOff)); err != nil {
		return nil, fmt.Errorf("dockerbrew: %s: read dynamic strings: %w", path, err)
	}
	str := func(off uint64) (string, bool) {
		if off >= uint64(len(table)) {
			return "", false
		}
		return cstring(table[off:]), true
	}
	for _, off := range needed {
		name, ok := str(off)
		if !ok || name == "" || strings.Contains(name, "/") {
			return nil, fmt.Errorf("dockerbrew: %s: DT_NEEDED %q must be a bare library name", path, name)
		}
	}
	// Strings may share storage through tail merging; a rewrite must not
	// change any other name that points into the slot.
	others = append(others, needed...)
	others = append(others, dynsymNameOffsets(ef)...)

	var writes []elfWrite
	for _, off := range paths {
		cur, ok := str(off)
		if !ok {
			return nil, fmt.Errorf("dockerbrew: %s: run path offset %d is outside the string table", path, off)
		}
		if cur == originLib {
			continue
		}
		if len(originLib) > len(cur) {
			return nil, fmt.Errorf("dockerbrew: %s: run path %q is shorter than %q", path, cur, originLib)
		}
		for _, o := range others {
			if o > off && o < off+uint64(len(cur)) {
				return nil, fmt.Errorf("dockerbrew: %s: run path %q shares storage with another name", path, cur)
			}
		}
		writes = append(writes, elfWrite{int64(strOff + off), padded(originLib, len(cur))})
	}
	return writes, nil
}

// dynsymNameOffsets returns the st_name of every dynamic symbol, all of which
// index the same string table as the dynamic section.
func dynsymNameOffsets(ef *elf.File) []uint64 {
	s := ef.SectionByType(elf.SHT_DYNSYM)
	if s == nil {
		return nil
	}
	data, err := s.Data()
	if err != nil {
		return nil
	}
	var out []uint64
	for i := 0; i+elf.Sym64Size <= len(data); i += elf.Sym64Size {
		out = append(out, uint64(binary.LittleEndian.Uint32(data[i:])))
	}
	return out
}

func fileOffset(ef *elf.File, vaddr uint64) (uint64, bool) {
	for _, p := range ef.Progs {
		if p.Type == elf.PT_LOAD && vaddr >= p.Vaddr && vaddr < p.Vaddr+p.Filesz {
			return vaddr - p.Vaddr + p.Off, true
		}
	}
	return 0, false
}

func cstring(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

func padded(s string, n int) []byte {
	out := make([]byte, n)
	copy(out, s)
	return out
}
