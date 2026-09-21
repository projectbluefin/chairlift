// Package commands owns the canonical application and window command
// inventory used by menus, shortcuts, and accelerator registration.
package commands

// Command describes one advertised command and its keyboard shortcut.
type Command struct {
	Action      string
	Accelerator string
	Display     string
	Title       string
	Group       string
	SystemOwned bool
}

// Binding groups the accelerators registered for one action.
type Binding struct {
	Action       string
	Accelerators []string
}

const (
	PreferencesAction   = "win.preferences"
	CheckAction         = "win.check"
	ShowShortcutsAction = "win.show-shortcuts"
	HelpAction          = "win.help"
	ShowAboutAction     = "win.show-about"
	QuitAction          = "app.quit"
	PrimaryMenuAction   = "app.menu"
)

var inventory = []Command{
	{
		Action:      PreferencesAction,
		Accelerator: "<Primary>comma",
		Display:     "Ctrl+,",
		Title:       "Preferences",
		Group:       "general",
	},
	{
		Action:      CheckAction,
		Accelerator: "<Primary>r",
		Display:     "Ctrl+R",
		Title:       "Check again",
		Group:       "general",
	},
	{
		Action:      ShowShortcutsAction,
		Accelerator: "<Primary>question",
		Display:     "Ctrl+?",
		Title:       "Keyboard Shortcuts",
		Group:       "general",
	},
	{
		Action:      HelpAction,
		Accelerator: "F1",
		Display:     "F1",
		Title:       "Help",
		Group:       "general",
	},
	{
		Action:      QuitAction,
		Accelerator: "<Primary>q",
		Display:     "Ctrl+Q",
		Title:       "Quit",
		Group:       "general",
	},
	{
		Action:      PrimaryMenuAction,
		Accelerator: "F10",
		Display:     "F10",
		Title:       "Open primary menu",
		Group:       "general",
		SystemOwned: true,
	},
}

// All returns a defensive copy of the complete command inventory.
func All() []Command {
	result := append([]Command(nil), inventory...)
	for index := range result {
		switch result[index].Action {
		case PreferencesAction:
			result[index].Display = "Ctrl+,"
			result[index].Title = "Preferences"
		case CheckAction:
			result[index].Display = "Ctrl+R"
			result[index].Title = "Check again"
		case ShowShortcutsAction:
			result[index].Display = "Ctrl+?"
			result[index].Title = "Keyboard Shortcuts"
		case HelpAction:
			result[index].Display = "F1"
			result[index].Title = "Help"
		case QuitAction:
			result[index].Display = "Ctrl+Q"
			result[index].Title = "Quit"
		case PrimaryMenuAction:
			result[index].Display = "F10"
			result[index].Title = "Open primary menu"
		}
	}
	return result
}

// Bindings returns one accelerator binding for each application-owned command.
// System-owned shortcuts, such as F10, are intentionally omitted.
func Bindings() []Binding {
	all := All()
	result := make([]Binding, 0, len(all))
	for _, command := range all {
		if command.SystemOwned {
			continue
		}
		result = append(result, Binding{
			Action:       command.Action,
			Accelerators: []string{command.Accelerator},
		})
	}
	return result
}
