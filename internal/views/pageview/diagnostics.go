package pageview

import (
	"fmt"
	"strings"
)

// DiagnosticsData holds system diagnostic facts to format and scrub.
type DiagnosticsData struct {
	OSName     string
	OSVersion  string
	ImageRef   string
	Kernel     string
	DesktopEnv string
	GPU        string
}

// SystemDiagnosticsRow returns the title and subtitle for the diagnostics row.
func SystemDiagnosticsRow() Row {
	return Row{
		Title:    "System diagnostics",
		Subtitle: "Copy scrubbed system facts to share in support requests",
	}
}

// DiagnosticsClipboardToast returns the toast message shown after copying diagnostics.
func DiagnosticsClipboardToast() string {
	return "System diagnostics copied to clipboard"
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
