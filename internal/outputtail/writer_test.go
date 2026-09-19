package outputtail

import (
	"strings"
	"testing"
)

func TestWriterKeepsOnlyTrailingBytes(t *testing.T) {
	w := New(5)

	if n, err := w.Write([]byte("abc")); err != nil || n != 3 {
		t.Fatalf("first Write = (%d, %v), want (3, nil)", n, err)
	}
	if got := w.String(); got != "abc" {
		t.Fatalf("after first write = %q, want abc", got)
	}

	if n, err := w.Write([]byte("defg")); err != nil || n != 4 {
		t.Fatalf("second Write = (%d, %v), want (4, nil)", n, err)
	}
	if got := w.String(); got != "cdefg" {
		t.Fatalf("after overflow write = %q, want cdefg", got)
	}
}

func TestWriterDropsOversizedWritePrefix(t *testing.T) {
	w := New(8)

	payload := "prefix-" + strings.Repeat("x", 16) + "tail"
	if n, err := w.Write([]byte(payload)); err != nil || n != len(payload) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(payload))
	}

	if got := w.String(); got != "xxxxtail" {
		t.Fatalf("retained tail = %q, want xxxxtail", got)
	}
}

func TestWriterWithZeroLimitDiscardsInput(t *testing.T) {
	w := New(0)

	if n, err := w.Write([]byte("diagnostics")); err != nil || n != len("diagnostics") {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len("diagnostics"))
	}
	if got := w.String(); got != "" {
		t.Fatalf("retained output = %q, want empty", got)
	}
}
