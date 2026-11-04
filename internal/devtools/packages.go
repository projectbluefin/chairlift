package devtools

// WSLPackage names the Homebrew package for the selected virtual machine engine.
// Like SetWSL, an unspecified backend uses the built-in engine.
func WSLPackage(backend string) (name string, cask bool) {
	if backend == BackendLima {
		return "lima", false
	}
	return "frostyard/tap/nsl", true
}
