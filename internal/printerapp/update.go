package printerapp

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// InstalledImage returns the Image= reference in an installed app's quadlet:
// the verified digest a current unit runs, or the moving tag a unit written
// before verification still carries. It reads one small file.
func InstalledImage(app App) (string, error) {
	path, err := UnitPath(app)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(content), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "Image" {
			if image := strings.TrimSpace(value); image != "" {
				return image, nil
			}
		}
	}
	return "", fmt.Errorf("printerapp: %s has no Image= line", app.UnitName())
}

// VerifiedImage verifies the family's configured reference against its
// publishing workflow and returns the digest-pinned reference Refresh would
// install. It writes nothing, so an update check can compare it with
// InstalledImage. A failure wraps ErrSignatureUnverified.
func VerifiedImage(ctx context.Context, app App) (string, error) {
	return verifiedImage(ctx, app.Family)
}
