// Package branding owns the product name.
//
// The application ships in Bluefin as "Control Center" while ChairLift remains
// the code name for the repository, the binaries, the packages, and the
// io.projectbluefin.chairlift application ID — the latter fixed by the pkexec
// exec-path annotations (ADR-0001) and the install prefix (ADR-0002). Two
// spellings therefore coexist permanently, and this package is the single
// owner of the one a user reads.
//
// It is deliberately dependency-free. The constant first lived in
// internal/views/pageview, which was already imported by internal/views and
// internal/window; then internal/notify and internal/config needed it too, and
// neither has any business importing a view-text package that transitively
// pulls in registry-SBOM parsing and Homebrew troubleshooting for one string.
package branding

// AppName is the product name shown to users: window title, navigation page,
// About dialog, menu items, notifications, and the fail-closed configuration
// toast. internal/installcheck asserts that no other string literal under
// internal/ or cmd/ reaches a user spelling the code name, and that the
// desktop entry's Name= matches this value.
const AppName = "Control Center"
