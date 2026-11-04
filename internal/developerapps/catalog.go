package developerapps

// Catalog returns the explicitly selected Linux packages. Homebrew entries use
// ublue-os only; Flatpak exceptions use their verified system remote. Toolbox
// covers the JetBrains family without duplicating individual IDE rows.
//
// Sources: ublue-os/homebrew-tap/Casks; flatpark/flatpark/registry;
// flathub/dev.zed.Zed. Sublime is intentionally excluded until ublue-os ships a
// Linux cask; Homebrew's upstream sublime-text cask is macOS-only.
func Catalog() []App {
	return []App{
		{
			ID: "vscode", Name: "Visual Studio Code", Description: "Microsoft's extensible code editor",
			Source: Homebrew, Package: "ublue-os/tap/visual-studio-code-linux", Architectures: []string{"amd64", "arm64"},
		},
		{
			ID: "vscodium", Name: "VSCodium", Description: "Community-built distribution of VS Code",
			Source: Homebrew, Package: "ublue-os/tap/vscodium-linux", Architectures: []string{"amd64", "arm64"},
		},
		{
			ID: "cursor", Name: "Cursor", Description: "AI-assisted code editor",
			Source: Flatpak, Package: "co.anysphere.cursor", Remote: "flatpark", Architectures: []string{"amd64", "arm64"},
		},
		{
			ID: "jetbrains-toolbox", Name: "JetBrains Toolbox", Description: "Install and manage JetBrains IDEs",
			Source: Homebrew, Package: "ublue-os/tap/jetbrains-toolbox-linux", Architectures: []string{"amd64"},
		},
		{
			ID: "antigravity", Name: "Antigravity", Description: "Google's agent-focused development environment",
			Source: Flatpak, Package: "com.google.Antigravity", Remote: "flatpark", Architectures: []string{"amd64"},
		},
		{
			ID: "zed", Name: "Zed", Description: "Code editor built for collaboration",
			Source: Flatpak, Package: "dev.zed.Zed", Remote: "flathub", Architectures: []string{"amd64", "arm64"},
		},
	}
}
