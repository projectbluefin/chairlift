package views

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/devtools"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

type developerOptionRow struct {
	kind      string
	tool      devtools.Tool
	row       *adw.ActionRow
	toggle    *guardedSwitch
	button    *gtk.Button
	spinner   *gtk.Spinner
	allowed   bool
	canEnable bool
	active    bool
}

func (uh *UserHome) buildDeveloperOptions(group *adw.PreferencesGroup, status ublue.Status) {
	if groupConfig := uh.config.GetGroupConfig("features_page", "dx_group"); groupConfig != nil {
		uh.wslBackend = groupConfig.WSLBackend
	}
	if uh.wslBackend == "" {
		uh.wslBackend = devtools.BackendNSL
	}

	for _, choice := range []struct{ kind, title, subtitle string }{
		{"wsl", "WSL Mode", "Persistent Linux machines with nsl inside lightweight systemd-vmspawn virtual machines. Runs as your user without host daemons."},
		{"docker", "Enable Docker", "Docker CLI, Compose, LazyDocker and Dive, with the base image's daemon. Requires administrator authentication; Docker access grants root-equivalent control."},
	} {
		item := &developerOptionRow{kind: choice.kind, row: adw.NewActionRow(), spinner: newActivitySpinner()}
		item.row.SetTitle(choice.title)
		item.row.SetSubtitle(choice.subtitle + " Checking availability…")
		item.row.AddSuffix(&item.spinner.Widget)
		item.toggle = newGuardedSwitch(false, func(enabled bool) { uh.onDeveloperOption(item, enabled) })
		item.toggle.widget.SetSensitive(false)
		item.row.AddSuffix(&item.toggle.widget.Widget)
		item.row.SetActivatableWidget(&item.toggle.widget.Widget)
		group.Add(&item.row.Widget)
		uh.developerOptions = append(uh.developerOptions, item)

		if choice.kind == "wsl" {
			combo := adw.NewComboRow()
			combo.SetTitle("WSL Backend")
			combo.SetSubtitle("Choose nsl (default) or Lima for virtual machines. An existing Lima machine selects Lima.")
			combo.SetModel(gtk.NewStringList([]string{"nsl (default)", "Lima"}))
			uh.wslSuppress = true
			if uh.wslBackend == devtools.BackendLima {
				combo.SetSelected(1)
			} else {
				combo.SetSelected(0)
			}
			uh.wslSuppress = false
			uh.wslCombo = combo
			uh.wslBackendNotify = func(_ gobject.Object, _ uintptr) {
				if uh.wslSuppress {
					return
				}
				selected := combo.GetSelected()
				target := devtools.BackendNSL
				if selected == 1 {
					target = devtools.BackendLima
				}
				if target == uh.wslBackend {
					return
				}
				uh.wslBackend = target
				uh.refreshDeveloperOptions(status)
			}
			combo.ConnectNotify(&uh.wslBackendNotify)
			group.Add(&combo.Widget)
		}
	}
	editors := adw.NewExpanderRow()
	editors.SetTitle("IDEs and terminal editors")
	editors.SetSubtitle("Choose only the tools you want; no hidden bundle is installed.")
	for _, tool := range devtools.Tools() {
		item := &developerOptionRow{kind: "tool", tool: tool, row: adw.NewActionRow(), spinner: newActivitySpinner(), button: gtk.NewButtonWithLabel("Install")}
		item.row.SetTitle(tool.Name)
		item.row.SetSubtitle("Checking installed state…")
		item.button.SetValign(gtk.AlignCenterValue)
		item.button.SetSensitive(false)
		item.row.AddSuffix(&item.spinner.Widget)
		item.row.AddSuffix(&item.button.Widget)
		uh.developerButtons.connect(item.button, func(gtk.Button) { uh.onDeveloperOption(item, true) })
		editors.AddRow(&item.row.Widget)
		uh.developerOptions = append(uh.developerOptions, item)
	}
	group.Add(&editors.Widget)
	// Initial reads share the action gate too; an older load cannot overwrite
	// a user's mutation or enable overlapping developer actions.
	uh.developerSwitch.SetSensitive(false)
	uh.refreshDeveloperOptions(status)
}

func (uh *UserHome) refreshDeveloperOptions(status ublue.Status) {
	if !uh.developerGate.TryStart() {
		return
	}
	uh.setDeveloperSensitive(false)
	backend := uh.wslBackend
	resolve := !uh.wslBackendResolved
	go func() {
		ctx, cancel := ublue.DefaultContext()
		defer cancel()
		var wsl devtools.WSLState
		var wslErr error
		var doctorOutput string
		var doctorErr error
		if backend == devtools.BackendNSL && resolve {
			// The backend choice is not stored: an existing Lima machine with
			// no nsl machine means the user chose Lima, so the first read
			// follows it instead of resetting to the default.
			nsl, nslErr := devtools.NSLStatus(ctx)
			lima, limaErr := devtools.LimaStatus(ctx)
			backend = devtools.ResolveBackend(backend, nslErr == nil && nsl.Exists, limaErr == nil && lima.Exists)
		}
		if backend == devtools.BackendLima {
			wsl, wslErr = devtools.LimaStatus(ctx)
		} else {
			wsl, wslErr = devtools.NSLStatus(ctx)
			if devtools.NSLInstalled() {
				doctorOutput, doctorErr = devtools.NSLDoctor(ctx)
			}
		}
		docker, dockerErr := devtools.DockerStatus(ctx)
		kvmExists, kvmAccess := devtools.KVMAvailable()
		formulae, formulaErr := homebrew.ListInstalledFormulae()
		casks, caskErr := homebrew.ListInstalledCasks()
		sgtk.RunOnMainThread(func() {
			defer uh.developerGate.Reset()
			if resolve {
				uh.wslBackendResolved = true
				if backend != uh.wslBackend {
					uh.wslBackend = backend
					if uh.wslCombo != nil {
						uh.wslSuppress = true
						uh.wslCombo.SetSelected(1)
						uh.wslSuppress = false
					}
				}
			}
			brew := uh.capabilities[capability.Homebrew]
			for _, item := range uh.developerOptions {
				switch item.kind {
				case "wsl":
					item.active = wsl.Running
					if backend == devtools.BackendLima {
						item.canEnable = devtools.HostSupported() && brew && kvmExists && (kvmAccess || status.Supports(ubluehelper.CommandKVMEnable))
						item.allowed = wslErr == nil && (wsl.Running || item.canEnable)
						item.toggle.set(item.active)
						item.row.SetSubtitle(wslSubtitle(backend, wsl, wslErr))
						if !wsl.Running && !devtools.HostSupported() {
							item.row.SetSubtitle("WSL Mode requires a Linux x86_64 or ARM64 host.")
						} else if !wsl.Running && !brew {
							item.row.SetSubtitle("Needs Homebrew to install Lima.")
						} else if !wsl.Running && !kvmExists {
							item.row.SetSubtitle("Needs hardware virtualization: /dev/kvm is missing. Enable virtualization in firmware.")
						} else if !wsl.Running && !kvmAccess {
							item.row.SetSubtitle("Needs hardware virtualization access. Enabling requests administrator authentication, then a new login before setup can continue.")
						}
					} else {
						// nsl backend
						nslInstalled := devtools.NSLInstalled()
						nslHostOk := devtools.NSLHostSupported()
						actionable, isKVM := devtools.ParseNSLDoctor(doctorOutput)

						if wsl.Running {
							// Stopping a running machine needs no prerequisite.
							item.canEnable = nslHostOk && nslInstalled
							item.allowed = wslErr == nil
							item.toggle.set(item.active)
							item.row.SetSubtitle(wslSubtitle(backend, wsl, wslErr))
						} else if !nslHostOk {
							item.canEnable = false
							item.allowed = false
							item.toggle.set(item.active)
							item.row.SetSubtitle("WSL Mode with nsl requires a Linux x86_64 host.")
						} else if !brew && !nslInstalled {
							item.canEnable = false
							item.allowed = false
							item.toggle.set(item.active)
							item.row.SetSubtitle("Needs Homebrew to install nsl.")
						} else if !kvmExists {
							item.canEnable = false
							item.allowed = false
							item.toggle.set(item.active)
							item.row.SetSubtitle("Needs hardware virtualization: /dev/kvm is missing. Enable virtualization in firmware.")
						} else if nslInstalled && doctorErr != nil && !isKVM && actionable != "" {
							item.canEnable = false
							item.allowed = false
							item.toggle.set(item.active)
							item.row.SetSubtitle(actionable)
						} else if !kvmAccess {
							item.canEnable = status.Supports(ubluehelper.CommandKVMEnable)
							item.allowed = item.canEnable
							item.toggle.set(item.active)
							item.row.SetSubtitle("Needs hardware virtualization access. Enabling requests administrator authentication, then a new login before setup can continue.")
						} else {
							item.canEnable = true
							item.allowed = wslErr == nil && (wsl.Running || item.canEnable)
							item.toggle.set(item.active)
							item.row.SetSubtitle(wslSubtitle(backend, wsl, wslErr))
						}
					}
				case "docker":
					item.active = docker.Active
					item.canEnable = docker.Available && brew && status.Supports(ubluehelper.CommandDockerEnable, ubluehelper.CommandDockerDisable)
					item.allowed = dockerErr == nil && status.Supports(ubluehelper.CommandDockerEnable, ubluehelper.CommandDockerDisable) && (docker.Active || item.canEnable)
					item.toggle.set(item.active)
					item.row.SetSubtitle(dockerSubtitle(docker, dockerErr))
					if !status.Supports(ubluehelper.CommandDockerEnable, ubluehelper.CommandDockerDisable) {
						item.row.SetSubtitle("Needs the installed Docker actions in the system helper. CLI tools alone do not provide a daemon.")
					}
				case "tool":
					packages, err := formulae, formulaErr
					if item.tool.Cask {
						packages, err = casks, caskErr
					}
					uh.applyDeveloperTool(item, packages, err, brew)
				}
			}
			uh.setDeveloperSensitive(true)
		})
	}()
}

func packagePresent(packages []homebrew.Package, name string) bool {
	for _, pkg := range packages {
		if pkg.Name == name || pkg.Name == filepath.Base(name) {
			return true
		}
	}
	return false
}

func (uh *UserHome) applyDeveloperTool(item *developerOptionRow, packages []homebrew.Package, err error, brew bool) {
	installed := packagePresent(packages, item.tool.Package)
	item.allowed = brew && item.tool.Supported() && err == nil && !installed
	switch {
	case !item.tool.Supported():
		item.row.SetSubtitle("Not available for this architecture.")
	case !brew:
		item.row.SetSubtitle("Needs Homebrew.")
	case err != nil:
		item.row.SetSubtitle("Could not check installed state; no installation is started.")
	case installed:
		item.button.SetLabel("Installed")
		item.row.SetSubtitle("Installed through Homebrew.")
	default:
		item.button.SetLabel("Install")
		item.row.SetSubtitle("Optional Homebrew tool; installed only when you choose it.")
	}
}

func (uh *UserHome) setDeveloperSensitive(sensitive bool) {
	if uh.developerSwitch != nil {
		uh.developerSwitch.SetSensitive(sensitive && uh.developerCanToggle)
	}
	if uh.wslCombo != nil {
		uh.wslCombo.SetSensitive(sensitive)
	}
	for _, item := range uh.developerOptions {
		if item.toggle != nil {
			item.toggle.widget.SetSensitive(sensitive && item.allowed)
		}
		if item.button != nil {
			item.button.SetSensitive(sensitive && item.allowed)
		}
	}
}

func wslSubtitle(backend string, state devtools.WSLState, err error) string {
	if backend == devtools.BackendLima {
		switch {
		case err != nil:
			return "Could not read the Ubuntu virtual machine's state."
		case state.Ready:
			return "Ubuntu is ready. Connect Remote SSH to lima-ubuntu; your home is shared writable. Install the Remote - SSH extension in your editor."
		case state.Running:
			return "Ubuntu is running, but its shell is not ready. Turn off to stop it without deleting data."
		case state.Exists:
			return "Ubuntu is stopped. Its disk and your files are kept; enabling starts it and autostart again."
		default:
			return "Create Ubuntu LTS with Lima, share your home writable and start it at login. First download: about 600 MB."
		}
	}
	switch {
	case err != nil:
		return "Could not read nsl machine state."
	case state.Ready:
		return "nsl is ready. Persistent Linux machines run as systemd-nspawn containers inside a lightweight VM."
	case state.Running:
		return "nsl is running, but its shell is not ready. Turn off to stop it without deleting data."
	case state.Exists:
		return "nsl is stopped. Machines and disks are kept; enabling starts the default machine again."
	default:
		return "Persistent Linux machines with nsl via systemd-vmspawn and QEMU/KVM. Runs as your user without host daemons."
	}
}

func dockerSubtitle(state devtools.DockerState, err error) string {
	switch {
	case err != nil:
		return "Could not read the system Docker daemon's state."
	case state.Ready:
		return "Docker's local daemon is ready for this account. Docker access grants root-equivalent control."
	case state.Active:
		return "Docker is running, but its socket is not accessible to this session. Log out and back in after granting access."
	case !state.Available:
		return "This base image has no Docker daemon. Installing CLI tools alone cannot run containers."
	default:
		return "Install Docker CLI, Compose, LazyDocker and Dive; start the system daemon. Needs your administrator password and a new login for daemon access."
	}
}

func (uh *UserHome) onDeveloperOption(item *developerOptionRow, enabled bool) {
	if !item.allowed || !uh.developerGate.TryStart() {
		return
	}
	before := item.row.GetSubtitle()
	uh.setDeveloperSensitive(false)
	setActivitySpinner(item.spinner, true)
	item.row.SetSubtitle("Preparing…")
	if item.button != nil {
		item.button.SetLabel("Installing…")
	}
	title := item.row.GetTitle()
	backend := uh.wslBackend
	go func() {
		// The worker carries no overall deadline on purpose: an nsl or Lima
		// first start downloads for longer than any read budget, and a
		// privileged helper call must not be cut mid-flight. Every step
		// instead carries its own bound — devtools and Homebrew commands
		// their read/mutation class timeout, and each status probe below
		// devtools' statusTimeout — so the completion below, which resets
		// developerGate and restores the controls, is reached without user
		// action except for an authentication prompt, which the user answers
		// or dismisses (#487).
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		progress := func(stage string) { sgtk.RunOnMainThread(func() { item.row.SetSubtitle(stage) }) }
		var err error
		var wsl devtools.WSLState
		var docker devtools.DockerState
		var stateErr error
		var packages []homebrew.Package
		switch item.kind {
		case "wsl":
			err = devtools.SetWSL(ctx, backend, enabled, progress)
			wsl, stateErr = devtools.WSLStatus(ctx, backend)
		case "docker":
			err = devtools.SetDocker(ctx, enabled, progress)
			docker, stateErr = devtools.DockerStatus(ctx)
		case "tool":
			progress("Installing " + item.tool.Name + " through Homebrew…")
			err = devtools.Install(item.tool)
			if item.tool.Cask {
				packages, stateErr = homebrew.ListInstalledCasks()
			} else {
				packages, stateErr = homebrew.ListInstalledFormulae()
			}
		}
		if err != nil && !errors.Is(err, devtools.ErrNewLogin) {
			log.Printf("views: %s failed: %v", title, err)
		}
		if stateErr != nil {
			log.Printf("views: developer option state read failed: %v", stateErr)
		}
		sgtk.RunOnMainThread(func() {
			defer uh.developerGate.Reset()
			setActivitySpinner(item.spinner, false)
			if dryrun.Enabled() {
				item.row.SetSubtitle(before)
				if item.toggle != nil {
					item.toggle.set(item.active)
				}
				if item.button != nil {
					item.button.SetLabel("Install")
				}
				if err == nil {
					uh.toastAdder.ShowToast("[DRY-RUN] Preview: " + item.row.GetTitle() + " — no changes made")
				}
			} else {
				switch item.kind {
				case "wsl":
					if stateErr == nil {
						item.active = wsl.Running
						item.allowed = item.active || item.canEnable
					}
					item.toggle.set(item.active)
					item.row.SetSubtitle(wslSubtitle(uh.wslBackend, wsl, stateErr))
				case "docker":
					if stateErr == nil {
						item.active = docker.Active
						item.allowed = item.active || item.canEnable
					}
					item.toggle.set(item.active)
					item.row.SetSubtitle(dockerSubtitle(docker, stateErr))
				case "tool":
					if stateErr == nil {
						uh.applyDeveloperTool(item, packages, nil, uh.capabilities[capability.Homebrew])
					} else {
						item.button.SetLabel("Install")
						item.row.SetSubtitle("Could not verify installed state; the previous observation is kept.")
					}
				}
				if errors.Is(err, devtools.ErrNewLogin) {
					item.row.SetSubtitle(pageview.KVMAccessNeedsNewLogin)
				} else if err != nil {
					item.row.SetSubtitle(item.row.GetSubtitle() + " Last action failed: " + strings.TrimSpace(err.Error()))
				} else if item.kind == "tool" && stateErr == nil && !packagePresent(packages, item.tool.Package) {
					item.row.SetSubtitle("Installation finished, but Homebrew has not reported this tool installed.")
				}
			}
			if errors.Is(err, devtools.ErrNewLogin) {
				uh.toastAdder.ShowToast(pageview.KVMAccessNeedsNewLogin)
			} else if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("%s: %v", item.row.GetTitle(), err))
			}
			if !dryrun.Enabled() && err == nil {
				go uh.loadHomebrewPackages()
			}
			uh.setDeveloperSensitive(true)
		})
	}()
}
