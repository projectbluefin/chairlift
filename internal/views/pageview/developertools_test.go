package pageview

import "testing"

func TestWSLEngineDocumentationURL(t *testing.T) {
	for _, tc := range []struct {
		backend string
		wantURL string
		wantTip string
	}{
		{"nsl", NSLDocumentationURL, "NSL documentation"},
		{"", NSLDocumentationURL, "NSL documentation"},
		{"other", NSLDocumentationURL, "NSL documentation"},
		{"lima", "https://lima-vm.io/docs/", "Lima documentation"},
	} {
		if got := WSLEngineDocumentationURL(tc.backend); got != tc.wantURL {
			t.Errorf("WSLEngineDocumentationURL(%q) = %q, want %q", tc.backend, got, tc.wantURL)
		}
		if got := WSLEngineDocumentationTooltip(tc.backend); got != tc.wantTip {
			t.Errorf("WSLEngineDocumentationTooltip(%q) = %q, want %q", tc.backend, got, tc.wantTip)
		}
	}
}
