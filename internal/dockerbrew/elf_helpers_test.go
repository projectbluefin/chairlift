package dockerbrew

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// elfSpec describes a minimal little-endian ELF64 file built from bytes, with
// the program headers and sections a real Homebrew bottle file carries.
type elfSpec struct {
	machine    elf.Machine
	interp     string // PT_INTERP contents; empty means no PT_INTERP
	interpSlot int    // PT_INTERP size including padding; 0 means len(interp)+1
	rpath      string
	runpath    string
	needed     []string
	soname     string
	// sonameInRpathTail points DT_SONAME at the last n bytes of the rpath
	// string, as a linker's tail merging would.
	sonameInRpathTail int
}

const elfBase = 0x400000

func buildELF(t *testing.T, s elfSpec) []byte {
	t.Helper()
	le := binary.LittleEndian
	machine := s.machine
	if machine == 0 {
		machine = elf.EM_X86_64
	}

	// Dynamic string table.
	dynstr := []byte{0}
	addStr := func(v string) uint64 {
		off := uint64(len(dynstr))
		dynstr = append(dynstr, v...)
		dynstr = append(dynstr, 0)
		return off
	}
	type dyn struct {
		tag elf.DynTag
		val uint64
	}
	var dyns []dyn
	for _, n := range s.needed {
		dyns = append(dyns, dyn{elf.DT_NEEDED, addStr(n)})
	}
	if s.rpath != "" {
		off := addStr(s.rpath)
		dyns = append(dyns, dyn{elf.DT_RPATH, off})
		if s.sonameInRpathTail > 0 {
			dyns = append(dyns, dyn{elf.DT_SONAME, off + uint64(len(s.rpath)-s.sonameInRpathTail)})
		}
	}
	if s.runpath != "" {
		dyns = append(dyns, dyn{elf.DT_RUNPATH, addStr(s.runpath)})
	}
	if s.soname != "" {
		dyns = append(dyns, dyn{elf.DT_SONAME, addStr(s.soname)})
	}
	hasDynamic := len(dyns) > 0

	shstr := []byte{0}
	addSh := func(v string) uint32 {
		off := uint32(len(shstr))
		shstr = append(shstr, v...)
		shstr = append(shstr, 0)
		return off
	}

	nph := 1
	if s.interp != "" {
		nph++
	}
	if hasDynamic {
		nph++
	}
	align8 := func(n int) int { return (n + 7) &^ 7 }
	off := 64 + nph*56
	interpOff, interpSize := 0, 0
	if s.interp != "" {
		interpSize = s.interpSlot
		if interpSize == 0 {
			interpSize = len(s.interp) + 1
		}
		interpOff = off
		off += interpSize
	}
	dynstrOff := off
	off += len(dynstr)
	off = align8(off)
	dynamicOff := off
	dynamicSize := 0
	if hasDynamic {
		dyns = append(dyns,
			dyn{elf.DT_STRTAB, elfBase + uint64(dynstrOff)},
			dyn{elf.DT_STRSZ, uint64(len(dynstr))},
			dyn{elf.DT_NULL, 0})
		dynamicSize = len(dyns) * 16
	}
	off += dynamicSize
	type sec struct {
		name                     uint32
		typ                      elf.SectionType
		flags                    elf.SectionFlag
		off, size, link, entsize int
	}
	secs := []sec{{}}
	if s.interp != "" {
		secs = append(secs, sec{addSh(".interp"), elf.SHT_PROGBITS, elf.SHF_ALLOC, interpOff, interpSize, 0, 0})
	}
	dynstrIdx := len(secs)
	secs = append(secs, sec{addSh(".dynstr"), elf.SHT_STRTAB, elf.SHF_ALLOC, dynstrOff, len(dynstr), 0, 0})
	if hasDynamic {
		secs = append(secs, sec{addSh(".dynamic"), elf.SHT_DYNAMIC, elf.SHF_ALLOC | elf.SHF_WRITE, dynamicOff, dynamicSize, dynstrIdx, 16})
	}
	shstrIdx := len(secs)
	shstrName := addSh(".shstrtab")
	shstrOff := off
	secs = append(secs, sec{shstrName, elf.SHT_STRTAB, 0, shstrOff, len(shstr), 0, 0})
	off += len(shstr)
	off = align8(off)
	shoff := off
	total := shoff + len(secs)*64

	b := make([]byte, total)
	copy(b, []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
	le.PutUint16(b[16:], uint16(elf.ET_DYN))
	le.PutUint16(b[18:], uint16(machine))
	le.PutUint32(b[20:], uint32(elf.EV_CURRENT))
	le.PutUint64(b[24:], elfBase)
	le.PutUint64(b[32:], 64)
	le.PutUint64(b[40:], uint64(shoff))
	le.PutUint16(b[52:], 64)
	le.PutUint16(b[54:], 56)
	le.PutUint16(b[56:], uint16(nph))
	le.PutUint16(b[58:], 64)
	le.PutUint16(b[60:], uint16(len(secs)))
	le.PutUint16(b[62:], uint16(shstrIdx))

	ph := 64
	putPh := func(typ elf.ProgType, flags elf.ProgFlag, o, size, align int) {
		le.PutUint32(b[ph:], uint32(typ))
		le.PutUint32(b[ph+4:], uint32(flags))
		le.PutUint64(b[ph+8:], uint64(o))
		le.PutUint64(b[ph+16:], elfBase+uint64(o))
		le.PutUint64(b[ph+24:], elfBase+uint64(o))
		le.PutUint64(b[ph+32:], uint64(size))
		le.PutUint64(b[ph+40:], uint64(size))
		le.PutUint64(b[ph+48:], uint64(align))
		ph += 56
	}
	if s.interp != "" {
		putPh(elf.PT_INTERP, elf.PF_R, interpOff, interpSize, 1)
		copy(b[interpOff:], s.interp)
	}
	putPh(elf.PT_LOAD, elf.PF_R|elf.PF_X, 0, total, 0x1000)
	if hasDynamic {
		putPh(elf.PT_DYNAMIC, elf.PF_R|elf.PF_W, dynamicOff, dynamicSize, 8)
		for i, d := range dyns {
			le.PutUint64(b[dynamicOff+i*16:], uint64(d.tag))
			le.PutUint64(b[dynamicOff+i*16+8:], d.val)
		}
	}
	copy(b[dynstrOff:], dynstr)
	copy(b[shstrOff:], shstr)
	for i, sc := range secs {
		p := shoff + i*64
		le.PutUint32(b[p:], sc.name)
		le.PutUint32(b[p+4:], uint32(sc.typ))
		le.PutUint64(b[p+8:], uint64(sc.flags))
		if sc.flags&elf.SHF_ALLOC != 0 {
			le.PutUint64(b[p+16:], elfBase+uint64(sc.off))
		}
		le.PutUint64(b[p+24:], uint64(sc.off))
		le.PutUint64(b[p+32:], uint64(sc.size))
		le.PutUint32(b[p+40:], uint32(sc.link))
		le.PutUint64(b[p+48:], 1)
		le.PutUint64(b[p+56:], uint64(sc.entsize))
	}

	// The builder is only useful if the standard parser reads it back.
	if _, err := elf.NewFile(bytes.NewReader(b)); err != nil {
		t.Fatalf("built ELF does not parse: %v", err)
	}
	return b
}

func writeTemp(t *testing.T, name string, data []byte, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// elfFacts is what the dynamic loader would read from a file.
type elfFacts struct {
	interp  string
	rpath   []string
	runpath []string
	needed  []string
	soname  []string
}

func readELFFacts(t *testing.T, p string) elfFacts {
	t.Helper()
	f, err := elf.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var facts elfFacts
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			data := make([]byte, prog.Filesz)
			if _, err := prog.ReadAt(data, 0); err != nil {
				t.Fatal(err)
			}
			facts.interp = cstring(data)
		}
	}
	get := func(tag elf.DynTag) []string {
		v, err := f.DynString(tag)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	facts.rpath = get(elf.DT_RPATH)
	facts.runpath = get(elf.DT_RUNPATH)
	facts.needed = get(elf.DT_NEEDED)
	facts.soname = get(elf.DT_SONAME)
	return facts
}
