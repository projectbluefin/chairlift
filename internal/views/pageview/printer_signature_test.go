package pageview

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/printerapp"
)

func TestPrinterSignatureFailureGivesActionableToast(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"missing verifier", fmt.Errorf("%w: %w", printerapp.ErrSignatureUnverified, exec.ErrNotFound), "Install cosign"},
		{"rejected signature", fmt.Errorf("%w: %w", printerapp.ErrSignatureUnverified, errors.New("rejected")), "signature could not be verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := PrinterAppFailureToast(true, "HP Printers", tc.err)
			if !strings.Contains(got, tc.want) || !strings.Contains(got, "HP Printers") || strings.Contains(got, "Try again") {
				t.Errorf("toast = %q", got)
			}
			if got := PrinterAppFailureToast(false, "HP Printers", tc.err); !strings.Contains(got, "Couldn't turn it off") {
				t.Errorf("disable toast = %q", got)
			}
		})
	}
}
