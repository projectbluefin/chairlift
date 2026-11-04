# Developer Mode subpage and hardware enablement

## Navigation and developer tools

`developer` is a detail child of `features`, gated by the existing
`features_page.dx_group` setting. `--dx-mode` targets that detail. Back returns
to Features; unavailable details fall back through the normal route resolver.

The detail owns Developer Mode, WSL Mode, Docker, and developer setup activity.
Gaming Mode and its independent activity remain on Features. Static page
structure reuses `page.blp`; configuration-dependent rows stay in Go.

The **Development Apps** catalog contains exactly six entries: Visual Studio
Code, VSCodium, and JetBrains Toolbox from `ublue-os` Homebrew taps only (never
`castrojo` or other personal taps); Cursor (`co.anysphere.cursor`) and Antigravity
(`com.google.Antigravity`) from system FlatPark Flatpaks; and Zed (`dev.zed.Zed`)
from system Flathub. Sublime Text is deliberately excluded for now by user
approval, not a blocked catalog requirement. Never invent package tokens.
GNOME Builder, Kate, and individual JetBrains IDE rows are also excluded.

Each row shows its source and installed status with an individual native Install
action, accessible as `Install <Name>`. State labels are `Checking…`, `Installed`,
`Unavailable` for a missing backend, remote, or unsupported architecture, and
`Installing…` during work. Failed inventory offers Retry without interpreting
the failure as absence. Antigravity and the current Toolbox cask require amd64;
the other four entries support amd64 and arm64. No Bazaar launcher or application
launch is needed. IDE installation does not require the Developer Mode toggle
to be enabled; all actions share the existing developer-operation gate.

## Hardware enablement

`internal/hardware` detects ASUS and Framework through DMI under
`/sys/class/dmi/id`, with `/sys/devices/virtual/dmi/id` fallback. Tests inject a
root directory; production's empty root means `/`, independent of the working
directory.

| Hardware | Universal Blue Homebrew casks |
| --- | --- |
| ASUS | `ublue-os/tap/asusctl-linux`, `ublue-os/tap/rog-control-center-linux` |
| Framework | `ublue-os/tap/framework-tool`, `ublue-os/tap/framework-wallpapers` |

Ordinary activation only reads installed status. Explicit initial setup installs
missing casks through the existing Homebrew runner, then re-reads inventory.
Every package must have an installed version before the UI displays Active.
Inventory/install errors remain failures. Dry-run previews do not synthesize
installed state. The Features hardware group is informational and hidden on
unsupported hardware.

After verified package installation, `livery.EnsurePanelDefault` can apply the
embedded ASUS or Framework mark. It reuses Livery's icon-theme installation,
extension availability checks, prior-setting capture, and selection persistence.
Existing choices and opt-outs remain unchanged. The OEM casks do not supply the
previously proposed `asus-rog-symbolic`/`framework-logo-symbolic` assets; actual
CC0 Simple Icons artwork is embedded instead, with source/license comments.

External operations run off the GTK main thread; results return through
`sgtk.RunOnMainThread`. A setup gate prevents overlapping OEM installs, and a
refresh generation prevents stale status reads from overwriting setup results.

## Verification

Headless hardware, Livery, navigation, developer-app, and installcheck tests cover
the pure behavior. `make ci` checks unit and race tests, lint, formatting, vet,
and both supported build architectures. The targeted `@developer-detail-smoke`
GTK scenario exercises the six native rows, accessible action names, back
navigation, and separate Developer/Gaming activity on an isolated Dakota runtime.
Live package verification must use a disposable Dakota environment and verify
the installed inventory rather than treating a successful command as proof.
