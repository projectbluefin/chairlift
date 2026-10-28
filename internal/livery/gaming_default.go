package livery

import (
	"context"
	"errors"
	"log"

	"github.com/projectbluefin/chairlift/internal/config"
)

// panelGroupEnabled reports whether the administrator's configuration shows
// the Livery page's Top Bar Icon section. It is a seam for tests; Load fails
// closed, so an unreadable configuration also suppresses the default.
var panelGroupEnabled = func() bool {
	cfg, _ := config.Load()
	return cfg.IsGroupEnabled("livery_page", "livery_foundation_group")
}

// EnsureGamingPanelDefault puts the Open Gaming Collective mark on the panel
// of a gaming image that has never been configured.
//
// Load only resolves which foundation the page preselects; panel-enabled
// defaults to false, so without this the mark never reached the panel until
// the user found and flipped the switch. An empty panel-foundation means the
// user has never chosen, and it is written here once the mark lands, so the
// default is applied a single time and a later opt-out stays an opt-out.
//
// It is a no-op off a gaming image, when the session has no panel surface or
// the Custom Command Menu extension is absent (the same PanelAvailable gate the
// Livery page uses, so nothing is half-written and retried on every launch),
// or when the schema is not installed. It is also a no-op when the
// configuration disables the Livery page's livery_foundation_group, since the
// user would then have no switch to turn the mark back off.
func EnsureGamingPanelDefault(ctx context.Context) error {
	if !detectGaming() || !panelGroupEnabled() || !PanelAvailable(ctx) {
		return nil
	}
	values, err := readAll(ctx)
	if err != nil {
		if errors.Is(err, ErrSchemaMissing) {
			return nil
		}
		return err
	}
	if values[KeyPanelID] != "" || parseBool(values[KeyPanelEnabled]) {
		return nil
	}

	if values[KeySavedPanelIcon] == "" && values[KeySavedPanelMode] == "" {
		icon, mode, ok := CapturePanelOverrides(ctx)
		if !ok {
			return errors.New("livery: reading the current panel icon: dconf is unavailable")
		}
		if err := SetString(ctx, KeySavedPanelIcon, icon); err != nil {
			return err
		}
		if err := SetString(ctx, KeySavedPanelMode, mode); err != nil {
			return err
		}
	}
	if err := Apply(ctx, Panel, Source{Kind: FromCatalog, Value: GamingDefaultID}); err != nil {
		return err
	}
	if err := SetBool(ctx, KeyPanelEnabled, true); err != nil {
		return err
	}
	log.Printf("livery: applied the %s panel mark on this gaming image", GamingDefaultID)
	return SetString(ctx, KeyPanelID, GamingDefaultID)
}
