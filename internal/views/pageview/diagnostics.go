package pageview

import (
	"fmt"
	"strings"
)

// DiagnosticsData holds system diagnostic facts to format and scrub.
// SystemVersion and Digest are the booted deployment's version and full
// image digest — the identifiers the Updates page's Details row shows — so a
// support request names the exact build rather than only its channel.
type DiagnosticsData struct {
	OSName        string
	OSVersion     string
	ImageRef      string
	SystemVersion string
	Digest        string
	Kernel        string
	DesktopEnv    string
	GPU           string
}

// SystemDiagnosticsRow returns the title and subtitle for the diagnostics row.
func SystemDiagnosticsRow() Row {
	return Row{
		Title:    "System diagnostics",
		Subtitle: "Copy details about this computer for a help request.",
	}
}

// DiagnosticsClipboardToast returns the toast message shown after copying diagnostics.
func DiagnosticsClipboardToast() string {
	return "Copied to the clipboard."
}

// FormatScrubbedDiagnostics formats system facts into a clean text block for support,
// scrubbing sensitive information such as usernames and home directories.
func FormatScrubbedDiagnostics(data DiagnosticsData, username, homedir string) string {
	var b strings.Builder
	b.WriteString("=== System Diagnostics ===\n")

	osLine := data.OSName
	if data.OSVersion != "" {
		if osLine != "" {
			osLine += " " + data.OSVersion
		} else {
			osLine = data.OSVersion
		}
	}
	if osLine == "" {
		osLine = "Unknown"
	}
	fmt.Fprintf(&b, "OS: %s\n", osLine)

	if data.ImageRef != "" {
		fmt.Fprintf(&b, "Image: %s\n", data.ImageRef)
	}
	if data.SystemVersion != "" {
		fmt.Fprintf(&b, "Version: %s\n", data.SystemVersion)
	}
	// The digest is written in full: a truncated digest cannot be matched
	// against a registry, which is the only thing a support reader does with it.
	if data.Digest != "" {
		fmt.Fprintf(&b, "Build ID: %s\n", data.Digest)
	}
	if data.Kernel != "" {
		fmt.Fprintf(&b, "Kernel: %s\n", data.Kernel)
	}
	if data.DesktopEnv != "" {
		fmt.Fprintf(&b, "Desktop: %s\n", data.DesktopEnv)
	}
	if data.GPU != "" {
		fmt.Fprintf(&b, "Graphics: %s\n", data.GPU)
	}

	raw := b.String()
	return scrubText(raw, username, homedir)
}

func scrubText(text, username, homedir string) string {
	res := text
	if homedir != "" && homedir != "/" {
		res = strings.ReplaceAll(res, homedir, "~")
	}
	if username != "" {
		res = strings.ReplaceAll(res, "/home/"+username, "~")
		res = strings.ReplaceAll(res, "/var/home/"+username, "~")
	}
	return res
}
