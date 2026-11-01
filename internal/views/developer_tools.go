package views

import (
	"context"
	"errors"
	"log"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/devmenu"
	"github.com/projectbluefin/chairlift/internal/devsetup"
	"github.com/projectbluefin/chairlift/internal/devtools"
	"github.com/projectbluefin/chairlift/internal/dockerbrew"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

type developerOptionRow struct {
	kind          string
	row           *adw.ExpanderRow
	toggle        *guardedSwitch
	spinner       *gtk.Spinner
	allowed       bool
	canEnable     bool
	canDisable    bool
	active        bool
	awaitingLogin bool
	packageRow    *adw.ActionRow
}

func (uh *UserHome) updateWSLPackage(item *developerOptionRow) {
	if item.packageRow == nil {
		return
	}
	name, cask := devtools.WSLPackage(uh.wslBackend)
	kind := homebrew.BundleItemBrew
	if cask {
		kind = homebrew.BundleItemCask
	}
	item.packageRow.SetTitle(name)
	item.packageRow.SetSubtitle(bundleview.ContentsKindLabel(kind))
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
		item := &developerOptionRow{kind: choice.kind, row: adw.NewExpanderRow(), spinner: newActivitySpinner()}
		item.row.SetUseMarkup(false)
		item.row.SetTitle(choice.title)
		item.row.SetSubtitle("Checking…")
		item.row.AddSuffix(&item.spinner.Widget)
		item.toggle = newGuardedSwitch(false, func(enabled bool) { uh.onDeveloperOption(item, enabled) })
		SetAccessibleLabel(item.toggle.widget, choice.title)
		item.toggle.widget.SetSensitive(false)
		item.row.AddSuffix(&item.toggle.widget.Widget)
		if choice.kind == "docker" {
			for _, name := range dockerbrew.Formulae() {
				addSetupContents(item.row, name, pageview.DockerRuntimeContents)
			}
			for _, name := range dockerbrew.CLIFormulae() {
				addSetupContents(item.row, name, pageview.DockerCLIContents)
			}
		} else {
			item.packageRow = addSetupContents(item.row, "", "")
			uh.updateWSLPackage(item)
		}
		group.Add(&item.row.Widget)
		uh.developerOptions = append(uh.developerOptions, item)

		if choice.kind == "wsl" {
			combo := adw.NewComboRow()
			combo.SetTitle("Virtual Machine Engine")
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

			docBtn := newIconButton("adw-external-link-symbolic", pageview.WSLEngineDocumentationTooltip(uh.wslBackend))
			docBtn.SetValign(gtk.AlignCenterValue)
			docClicked := func(_ gtk.Button) {
				uh.openURL(pageview.WSLEngineDocumentationURL(uh.wslBackend))
			}
			docBtn.ConnectClicked(&docClicked)
			combo.AddSuffix(&docBtn.Widget)
			uh.wslDocBtn = docBtn

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
				uh.updateWSLPackage(item)
				if uh.wslDocBtn != nil {
					tip := pageview.WSLEngineDocumentationTooltip(target)
					uh.wslDocBtn.SetTooltipText(tip)
					SetAccessibleLabel(uh.wslDocBtn, tip)
				}
				uh.refreshDeveloperOptions(status)
			}
			combo.ConnectNotify(&uh.wslBackendNotify)
			item.row.AddRow(&combo.Widget)
		}
	}
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
		var nslGroupErr error
		if backend == devtools.BackendNSL && kvmExists {
			member, err := devtools.NSLKVMGroupMember()
			if err != nil {
				log.Printf("views: nsl group check failed: %v", err)
			}
			nslGroupErr = err
			kvmAccess = kvmAccess && err == nil && member
		}
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
					if uh.wslDocBtn != nil {
						tip := pageview.WSLEngineDocumentationTooltip(backend)
						uh.wslDocBtn.SetTooltipText(tip)
						SetAccessibleLabel(uh.wslDocBtn, tip)
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
				uh.updateWSLPackage(item)
			}
			for _, item := range uh.developerOptions {
				switch item.kind {
				case "wsl":
					if wslErr == nil {
						item.active = wsl.Running
					}
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
						} else if nslGroupErr != nil {
							item.canEnable = false
							item.allowed = false
							item.toggle.set(item.active)
							item.row.SetSubtitle(devtools.NeedsPrerequisite)
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
					// A running daemon this account cannot reach still counts as
					// observed; it reads off so the switch dispatches enable.
					observed := dockerErr == nil || docker.Active
					if observed {
						item.active = pageview.DockerSwitchOn(docker)
					}
					item.canEnable = docker.Available && status.Supports(ubluehelper.CommandDockerEnable)
					item.canDisable = status.Supports(ubluehelper.CommandDockerDisable)
					item.allowed = observed && pageview.DockerControlAllowed(item.active, item.canEnable, item.canDisable)
					item.toggle.set(item.active)
					item.row.SetSubtitle(pageview.DockerSubtitle(docker, dockerErr))
					if observed && !item.allowed && docker.Unavailable == "" && !docker.NeedsLogin {
						item.row.SetSubtitle("Docker controls aren't available on this computer.")
					}
				}
				if !item.active && !uh.developerEnabled && !uh.developerCanToggle {
					item.allowed = false
					item.row.SetSubtitle("Developer Mode must be enabled first, but its administrator action isn't installed.")
				}
			}
			uh.setDeveloperSensitive(true)
		})
	}()
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
	}
}

func wslSubtitle(backend string, state devtools.WSLState, err error) string {
	if backend == devtools.BackendLima {
		switch {
		case err != nil:
			return "Couldn't check the Ubuntu virtual machine."
		case state.Ready:
			return "Ubuntu is ready in Lima. Run limactl shell ubuntu, or connect your editor over SSH to lima-ubuntu."
		case state.Running:
			return "Ubuntu is running but not ready yet."
		case state.Exists:
			return "Ubuntu is stopped. Your files are kept."
		default:
			return "Run Ubuntu LTS in a Lima virtual machine with writable access to your home folder. Downloads about 600 MB. Open a terminal with limactl shell ubuntu."
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

func (uh *UserHome) onDeveloperOption(item *developerOptionRow, enabled bool) {
	uh.runDeveloperOption(item, enabled, false, enabled && !uh.developerEnabled)
}

func (uh *UserHome) runDeveloperOption(item *developerOptionRow, enabled, retry, post bool) {
	allowed := item.allowed
	if retry {
		if item.kind == "docker" {
			allowed = (enabled && item.canEnable) || (!enabled && item.canDisable)
		} else {
			// A failed read must not erase the original prerequisites or the
			// last observed running machine's ability to stop.
			allowed = (enabled && item.canEnable) || (!enabled && item.active)
		}
	}
	if !allowed || !uh.developerGate.TryStart() {
		sgtk.RunOnMainThread(func() { item.toggle.set(item.active) })
		return
	}
	request := devsetup.ChildRequest(uh.developerEnabled, enabled, dryrun.Enabled())
	if request.ChangeDeveloper && !uh.developerCanToggle {
		uh.developerGate.Reset()
		sgtk.RunOnMainThread(func() { item.toggle.set(item.active) })
		return
	}
	before, title, backend := item.row.GetSubtitle(), item.row.GetTitle(), uh.wslBackend
	ctx := context.Background() // Providers bound each command, not the full download.
	var operationErr, stateErr, editorErr error
	var previous map[devsetup.Stage]bool
	var observed bool
	var wsl devtools.WSLState
	var docker devtools.DockerState
	awaitingLogin := item.awaitingLogin
	progress := func(stage string) {
		uh.setupActivity.detail(stage)
		sgtk.RunOnMainThread(func() { item.row.SetSubtitle(stage) })
	}
	tasks := []devsetup.Task{
		{Stage: devsetup.Tools, Run: func() error {
			switch item.kind {
			case "wsl":
				if enabled && !request.Preview && awaitingLogin {
					if _, accessible := devtools.KVMAvailable(); !accessible {
						operationErr = devtools.ErrNewLogin
						return devsetup.ErrAwaitingLogin
					}
					awaitingLogin = false
				}
				operationErr = devtools.SetWSL(ctx, backend, enabled, progress)
				if errors.Is(operationErr, devtools.ErrNewLogin) {
					awaitingLogin = true
					return devsetup.ErrAwaitingLogin
				}
			case "docker":
				if retry && enabled && !request.Preview && previous[devsetup.Tools] {
					// A completed activation is not repeated to acquire groups in
					// this process. Observe first; only a daemon this account
					// cannot use yet needs enable.
					docker, stateErr = devtools.DockerStatus(ctx)
					if docker.Ready || docker.NeedsLogin {
						return nil
					}
					if stateErr != nil && !docker.NoAccess {
						operationErr = stateErr
						return operationErr
					}
				}
				operationErr = devtools.SetDocker(ctx, enabled, progress)
				if errors.Is(operationErr, devtools.ErrDockerNewLogin) {
					// The helper enabled Docker; Verify still awaits login. A
					// CLI that failed to install is still this stage's failure.
					var cliErr *devtools.DockerCLIError
					if errors.As(operationErr, &cliErr) {
						return cliErr
					}
					return nil
				}
			}
			return operationErr
		}},
		{Stage: devsetup.Verify, Run: func() error {
			observed = true
			var ready bool
			switch item.kind {
			case "wsl":
				wsl, stateErr = devtools.WSLStatus(ctx, backend)
				if enabled && !request.Preview && errors.Is(operationErr, devtools.ErrNewLogin) {
					return devsetup.ErrAwaitingLogin
				}
				ready = (!enabled && !wsl.Running) || (enabled && wsl.Ready)
			case "docker":
				docker, stateErr = devtools.DockerStatus(ctx)
				if enabled && !request.Preview && (docker.NeedsLogin || errors.Is(stateErr, devtools.ErrDockerNewLogin)) {
					return devsetup.ErrAwaitingLogin
				}
				ready = (!enabled && !docker.Active) || (enabled && docker.Ready)
				if request.Preview && docker != (devtools.DockerState{}) {
					// A preview changed nothing; once the units were read, a
					// socket this session cannot ping says nothing about it.
					stateErr = nil
				}
			}
			if stateErr != nil {
				return stateErr
			}
			if !request.Preview && !ready {
				return errors.New("the requested service state is not ready")
			}
			return nil
		}},
	}
	if item.kind == "docker" && enabled {
		tasks = append(tasks, devsetup.Task{Stage: devsetup.VSCode, Optional: true, Run: func() error {
			if operationErr != nil && !errors.Is(operationErr, devtools.ErrDockerNewLogin) {
				return devsetup.ErrSkipped
			}
			err := devtools.SetupVSCode(ctx, progress)
			if errors.Is(err, devtools.ErrVSCodeNotInstalled) {
				return devsetup.ErrSkipped // No editor to set up is not a failure.
			}
			editorErr = err
			return editorErr
		}})
	}
	if post {
		tasks = append(tasks, uh.developerPostTasks(true, true)...)
	}
	if !uh.setupActivity.begin(title, setupStages(request.ChangeDeveloper, tasks), retry, func() {
		uh.runDeveloperOption(item, enabled, true, post)
	}) {
		uh.developerGate.Reset()
		sgtk.RunOnMainThread(func() { item.toggle.set(item.active) })
		return
	}
	previous = uh.setupActivity.completed
	uh.setDeveloperSensitive(false)
	setActivitySpinner(item.spinner, true)
	item.row.SetSubtitle("Preparing…")
	go func() {
		var menuErr error
		var grantSucceeded bool
		var skipped []string
		result := devsetup.Run(ctx, request, func() error {
			helperCtx, cancel := ublue.DefaultContext()
			defer cancel()
			var err error
			skipped, err = ublue.SetDeveloperMode(helperCtx, true)
			grantSucceeded = err == nil
			return err
		}, tasks, previous, uh.developerSetupReport(true))
		if grantSucceeded {
			menuCtx, cancel := ublue.DefaultContext()
			menuErr = devmenu.Apply(menuCtx, true)
			cancel()
		}
		sgtk.RunOnMainThread(func() {
			defer uh.developerGate.Reset()
			setActivitySpinner(item.spinner, false)
			uh.developerEnabled = result.Developer
			if !request.Preview {
				item.awaitingLogin = awaitingLogin
			}
			if uh.developerToggle != nil {
				uh.developerToggle.set(result.Developer)
			}
			if result.Promoted && uh.developerRow != nil {
				uh.developerRow.SetSubtitle(pageview.DeveloperResultSubtitle(true))
				uh.openDeveloperOnboarding(true, true)
				uh.toastAdder.ShowToast(actionmsg.DeveloperMode(false, true, skipped).Toast)
			}
			if request.Preview || !observed {
				item.row.SetSubtitle(before)
			} else {
				switch item.kind {
				case "wsl":
					if stateErr == nil {
						item.active = wsl.Running
					}
					item.allowed = stateErr == nil && (item.active || item.canEnable)
					item.row.SetSubtitle(wslSubtitle(backend, wsl, stateErr))
				case "docker":
					if stateErr == nil || docker.Active {
						item.active = pageview.DockerSwitchOn(docker)
					}
					item.allowed = (stateErr == nil || docker.Active) && pageview.DockerControlAllowed(item.active, item.canEnable, item.canDisable)
					item.row.SetSubtitle(pageview.DockerSubtitle(docker, stateErr))
				}
				if errors.Is(operationErr, devtools.ErrDockerNewLogin) {
					item.row.SetSubtitle(pageview.DockerSubtitle(docker, operationErr))
				} else if errors.Is(operationErr, devtools.ErrNewLogin) {
					item.row.SetSubtitle(pageview.KVMAccessNeedsNewLogin)
				} else if operationErr != nil && !pkexec.IsAuthDismissed(operationErr) {
					item.row.SetSubtitle(developerOptionFailure(item, enabled, operationErr))
				}
			}
			item.toggle.set(item.active)
			uh.setupActivity.finish(result, request.Preview)
			uh.setDeveloperSensitive(true)
			if menuErr != nil {
				log.Printf("views: developer menu update failed: %v", menuErr)
				uh.toastAdder.ShowErrorToast("Couldn't update the developer entries in the top bar menu.")
			}
			if !request.Preview && len(result.Completed) > 0 {
				uh.homebrewInventoryChanged()
			}
			if (item.kind == "docker" && result.AwaitingLogin) || errors.Is(operationErr, devtools.ErrDockerNewLogin) {
				uh.toastAdder.ShowToast(pageview.DockerSubtitle(docker, devtools.ErrDockerNewLogin))
			} else if errors.Is(operationErr, devtools.ErrNewLogin) {
				uh.toastAdder.ShowToast(pageview.KVMAccessNeedsNewLogin)
			} else if result.Err != nil && editorErr == nil {
				log.Printf("views: %s setup failed: %v", title, result.Err)
				uh.toastAdder.ShowErrorToast(pkexec.UserMessage(result.Err, title+" setup needs attention. Retry the failed steps."))
			} else if request.Preview && editorErr == nil {
				uh.toastAdder.ShowToast("[DRY-RUN] Preview: " + title + " — no changes made")
			}
			if editorErr != nil {
				log.Printf("views: VS Code setup failed: %v", editorErr)
				uh.toastAdder.ShowErrorToast(pageview.DockerEditorFailure(request.Preview))
			}
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
	case "docker":
		name = "Docker"
	}
	if enabled {
		return "Couldn't turn on " + name + ". Try again."
	}
	return "Couldn't turn off " + name + ". Try again."
}
