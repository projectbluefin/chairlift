package views

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/devtools"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/pkexec"
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
	// toolView is a tool row's last observed presentation; a dry run
	// restores it after showing the transient installing state.
	toolView pageview.DeveloperToolView
}

func (uh *UserHome) buildDeveloperOptions(group *adw.PreferencesGroup, status ublue.Status) {
	if groupConfig := uh.config.GetGroupConfig("features_page", "dx_group"); groupConfig != nil {
		uh.wslBackend = groupConfig.WSLBackend
	}
	if uh.wslBackend == "" {
		uh.wslBackend = devtools.BackendNSL
	}

	for _, choice := range []struct{ kind, title string }{
		{"wsl", "WSL Mode"},
		{"docker", "Enable Docker"},
	} {
		item := &developerOptionRow{kind: choice.kind, row: adw.NewActionRow(), spinner: newActivitySpinner()}
		item.row.SetTitle(choice.title)
		item.row.SetSubtitle("Checking…")
		item.row.AddSuffix(&item.spinner.Widget)
		item.toggle = newGuardedSwitch(false, func(enabled bool) { uh.onDeveloperOption(item, enabled) })
		item.toggle.widget.SetSensitive(false)
		item.row.AddSuffix(&item.toggle.widget.Widget)
		item.row.SetActivatableWidget(&item.toggle.widget.Widget)
		group.Add(&item.row.Widget)
		uh.developerOptions = append(uh.developerOptions, item)

		if choice.kind == "wsl" {
			combo := adw.NewComboRow()
			combo.SetTitle("Virtual machine engine")
			combo.SetSubtitle(devtools.EngineSubtitle(""))
			combo.SetModel(gtk.NewStringList([]string{"Built-in (recommended)", "Lima"}))
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
	editors.SetSubtitle("Install only the tools you want.")
	for _, tool := range devtools.Tools() {
		item := &developerOptionRow{kind: "tool", tool: tool, row: adw.NewActionRow(), spinner: newActivitySpinner(), button: gtk.NewButtonWithLabel("Install")}
		item.row.SetTitle(tool.Name)
		item.toolView = pageview.DeveloperToolChecking(tool.Name, tool.Description)
		uh.showDeveloperTool(item, item.toolView)
		item.button.SetValign(gtk.AlignCenterValue)
		item.button.SetSensitive(false)
		item.row.AddSuffix(&item.spinner.Widget)
		item.row.AddSuffix(&item.button.Widget)
		uh.developerButtons.connect(item.button, func(gtk.Button) { uh.onDeveloperOption(item, true) })
		editors.AddRow(&item.row.Widget)
		uh.developerOptions = append(uh.developerOptions, item)
	}
	group.Add(&editors.Widget)
	// Homebrew changes outside this page — an uninstall on Apps, a brew
	// command in a terminal — would otherwise leave a row reading
	// "Installed" until restart. Re-read whenever the group is shown; the
	// handler is connected once, here, for the page's lifetime.
	uh.developerToolsMapped = func(gtk.Widget) { uh.refreshDeveloperTools() }
	group.ConnectMap(&uh.developerToolsMapped)
	// Initial reads share the action gate too; an older load cannot overwrite
	// a user's mutation or enable overlapping developer actions.
	uh.developerSwitch.SetSensitive(false)
	uh.refreshDeveloperOptions(status)
}

// refreshDeveloperTools re-reads only the optional tools' installed state. It
// is passive: it stands aside while any developer action holds the gate, and
// every gated action that publishes tool state begins a new generation, so a
// read that started before that action can never overwrite its result.
func (uh *UserHome) refreshDeveloperTools() {
	if uh.developerGate.Running() || !slices.ContainsFunc(uh.developerOptions, func(item *developerOptionRow) bool { return item.kind == "tool" }) {
		return
	}
	generation := uh.developerToolRefresh.Begin()
	go func() {
		formulae, formulaErr := homebrew.ListInstalledFormulae()
		casks, caskErr := homebrew.ListInstalledCasks()
		sgtk.RunOnMainThread(func() {
			if !uh.developerToolRefresh.IsCurrent(generation) || uh.developerGate.Running() {
				return
			}
			brew := uh.capabilities[capability.Homebrew]
			for _, item := range uh.developerOptions {
				if item.kind != "tool" {
					continue
				}
				packages, err := formulae, formulaErr
				if item.tool.Cask {
					packages, err = casks, caskErr
				}
				uh.applyDeveloperTool(item, packages, err, brew)
				item.button.SetSensitive(item.allowed)
			}
		})
	}()
}

func (uh *UserHome) refreshDeveloperOptions(status ublue.Status) {
	if !uh.developerGate.TryStart() {
		return
	}
	uh.developerToolRefresh.Begin()
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
			if backend == devtools.BackendNSL && wslErr == nil && uh.wslCombo != nil {
				// An older ChairLift created a Debian machine; say so
				// rather than promise Ubuntu for the machine WSL Mode starts.
				uh.wslCombo.SetSubtitle(devtools.EngineSubtitle(wsl.Machine))
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
							item.row.SetSubtitle("WSL Mode doesn't work on this kind of computer.")
						} else if !wsl.Running && !brew {
							item.row.SetSubtitle("Lima can't be installed on this computer.")
						} else if !wsl.Running && !kvmExists {
							item.row.SetSubtitle(devtools.NeedsVirtualization)
						} else if !wsl.Running && !kvmAccess {
							item.row.SetSubtitle(devtools.NeedsKVMAccess)
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
							item.row.SetSubtitle("The built-in engine doesn't work on this kind of computer. Try Lima.")
						} else if !brew && !nslInstalled {
							item.canEnable = false
							item.allowed = false
							item.toggle.set(item.active)
							item.row.SetSubtitle("The built-in engine can't be installed on this computer.")
						} else if !kvmExists {
							item.canEnable = false
							item.allowed = false
							item.toggle.set(item.active)
							item.row.SetSubtitle(devtools.NeedsVirtualization)
						} else if nslInstalled && doctorErr != nil && !isKVM && actionable != "" {
							log.Printf("views: nsl doctor reported missing prerequisites: %s", strings.TrimSpace(doctorOutput))
							item.canEnable = false
							item.allowed = false
							item.toggle.set(item.active)
							item.row.SetSubtitle(actionable)
						} else if !kvmAccess {
							item.canEnable = status.Supports(ubluehelper.CommandKVMEnable)
							item.allowed = item.canEnable
							item.toggle.set(item.active)
							item.row.SetSubtitle(devtools.NeedsKVMAccess)
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
						item.row.SetSubtitle("Docker isn't available on this computer.")
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
	item.toolView = pageview.DeveloperTool(item.tool.Name, item.tool.Description, pageview.DeveloperToolObservation{
		Supported:  item.tool.Supported(),
		Homebrew:   brew,
		ReadFailed: err != nil,
		Installed:  err == nil && packagePresent(packages, item.tool.Package),
	})
	item.allowed = item.toolView.Allowed
	uh.showDeveloperTool(item, item.toolView)
}

// showDeveloperTool renders a tool row's subtitle and button. The visible
// label is the same word on every row, so the accessible name carries the
// tool's name. GtkButton names itself from its label child through a
// LABELLED_BY relation, which outranks the LABEL property, so the relation
// is dropped after every label change or every row would announce "Install".
func (uh *UserHome) showDeveloperTool(item *developerOptionRow, view pageview.DeveloperToolView) {
	item.row.SetSubtitle(view.Subtitle)
	item.button.SetLabel(view.ButtonLabel)
	item.button.ResetRelation(gtk.AccessibleRelationLabelledByValue)
	SetAccessibleLabel(item.button, view.AccessibleLabel)
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
			return "Couldn't check the Ubuntu virtual machine."
		case state.Ready:
			return "Ubuntu is ready. In your editor, connect with Remote SSH to lima-ubuntu."
		case state.Running:
			return "Ubuntu is running but not ready yet."
		case state.Exists:
			return "Ubuntu is stopped. Your files are kept."
		default:
			return "Run Ubuntu in a virtual machine that shares your home folder. Downloads about 600 MB."
		}
	}
	switch {
	case err != nil:
		return "Couldn't check your Linux virtual machine."
	case state.Ready:
		return "Your Linux virtual machine is ready."
	case state.Running:
		return "Your Linux virtual machine is running but not ready yet."
	case state.Exists:
		return "Your Linux virtual machine is stopped. Your files are kept."
	default:
		return "Run Linux in a virtual machine that keeps your files."
	}
}

func dockerSubtitle(state devtools.DockerState, err error) string {
	switch {
	case err != nil:
		return "Couldn't check whether Docker is running."
	case state.Ready:
		return "Docker is ready. Anyone using Docker can control this whole computer."
	case state.Active:
		return "Docker is running. Log out and back in to use it."
	case !state.Available:
		return "Docker isn't available on this system, so containers can't run here."
	default:
		return "Asks for your administrator password. Anyone using Docker can control this whole computer."
	}
}

func (uh *UserHome) onDeveloperOption(item *developerOptionRow, enabled bool) {
	if !item.allowed || !uh.developerGate.TryStart() {
		return
	}
	if item.kind == "tool" {
		uh.developerToolRefresh.Begin()
	}
	before := item.row.GetSubtitle()
	uh.setDeveloperSensitive(false)
	setActivitySpinner(item.spinner, true)
	if item.kind == "tool" {
		uh.showDeveloperTool(item, pageview.DeveloperToolInstalling(item.tool.Name))
	} else {
		item.row.SetSubtitle("Preparing…")
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
				if item.kind == "tool" {
					uh.showDeveloperTool(item, item.toolView)
				} else {
					item.row.SetSubtitle(before)
				}
				if item.toggle != nil {
					item.toggle.set(item.active)
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
						view := pageview.DeveloperToolUnverified(item.tool.Name, item.tool.Description)
						item.allowed = view.Allowed
						uh.showDeveloperTool(item, view)
					}
				}
				if errors.Is(err, devtools.ErrNewLogin) {
					item.row.SetSubtitle(pageview.KVMAccessNeedsNewLogin)
				} else if err != nil && !pkexec.IsAuthDismissed(err) {
					item.row.SetSubtitle(developerOptionFailure(item, enabled, err))
				} else if item.kind == "tool" && stateErr == nil && !packagePresent(packages, item.tool.Package) {
					item.row.SetSubtitle(item.tool.Description + " Installation finished, but Homebrew has not reported this tool installed.")
				}
			}
			if errors.Is(err, devtools.ErrNewLogin) {
				uh.toastAdder.ShowToast(pageview.KVMAccessNeedsNewLogin)
			} else if err != nil && item.kind == "tool" {
				uh.toastAdder.ShowErrorToast(developerOptionFailure(item, enabled, err))
			} else if err != nil {
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(err, developerOptionFailure(item, enabled, err)))
			}
			if !dryrun.Enabled() && err == nil {
				uh.homebrewInventoryChanged()
			}
			uh.setDeveloperSensitive(true)
		})
	}()
}

// developerOptionFailure is the plain sentence for a developer option that
// could not do what was asked. The raw error names commands and is logged by
// onDeveloperOption instead; a missing prerequisite shows its own sentence,
// since trying again would not help.
func developerOptionFailure(item *developerOptionRow, enabled bool, err error) string {
	var prerequisite *devtools.PrerequisiteError
	if errors.As(err, &prerequisite) {
		return prerequisite.Message
	}
	name := "WSL Mode"
	switch item.kind {
	case "tool":
		return "Couldn't install " + item.tool.Name + ". Try again."
	case "docker":
		name = "Docker"
	}
	if enabled {
		return "Couldn't turn on " + name + ". Try again."
	}
	return "Couldn't turn off " + name + ". Try again."
}
