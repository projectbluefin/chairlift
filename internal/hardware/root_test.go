package hardware

import (
	"path/filepath"
	"testing"
)

func TestDefaultSysfsRootIsAbsolute(t *testing.T) {
	if got := SysfsPath("", "sys_vendor"); !filepath.IsAbs(got) {
		t.Fatalf("default DMI path %q is relative to the application working directory", got)
	}
}
