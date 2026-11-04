package pageview

// Documentation URLs for WSL Mode virtual machine engines.
const (
	NSLDocumentationURL  = "https://frostyard.github.io/nsl/"
	LimaDocumentationURL = "https://lima-vm.io/docs/"
)

// WSLEngineDocumentationURL returns the documentation URL for the given WSL engine backend.
func WSLEngineDocumentationURL(backend string) string {
	if backend == "lima" {
		return LimaDocumentationURL
	}
	return NSLDocumentationURL
}

// WSLEngineDocumentationTooltip returns the tooltip and accessible label for the documentation button.
func WSLEngineDocumentationTooltip(backend string) string {
	if backend == "lima" {
		return "Lima documentation"
	}
	return "NSL documentation"
}
