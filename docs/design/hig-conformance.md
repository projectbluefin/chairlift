# GNOME HIG conformance

Living document. Rationale for the Blueprint mechanism:
[ADR-0022](../adr/0022-describe-static-ui-in-blueprint.md).

## Overview

ChairLift ships in Bluefin as Control Center, beside GNOME Settings and GNOME
Software, so it should look and behave like them. This document records how
the UI measures against the [GNOME HIG](https://developer.gnome.org/hig/) as
compiled by [gnome-gui-spec](https://github.com/hanthor/gnome-gui-spec): its
`skills/hig-audit` categories, its token files, and its source audits of GNOME
applications (Settings, Software, Disk Utility, Font Viewer, and others). It
lists what conforms, and every open finding with the reason it is open.

The audit was run in October 2026 against libadwaita 1.9 and GTK 4.24 (the
Dakota image) with a supported floor of libadwaita 1.6 (Bluefin LTS).

## Design

### Window structure

The window follows GNOME Settings' shape, described in `internal/ui/window.blp`:

```
AdwApplicationWindow (360×294 minimum)
└─ AdwToastOverlay
   └─ AdwToolbarView ── bottom bar: setup flow (Dismiss Setup · Back · Next)
      └─ GtkStack: pages │ welcome │ conclusion
         └─ AdwNavigationSplitView (collapses below 550sp)
            ├─ sidebar: header bar + primary menu, navigation-sidebar list
            │  of nav-row.blp rows (GNOME Settings' panel-row metrics)
            └─ content: one AdwNavigationPage over a stack of destinations,
               each an AdwToolbarView: header bar over an AdwPreferencesPage
```

- Each destination is a header bar over one `AdwPreferencesPage`, which
  scrolls and clamps itself (`internal/ui/page.blp`).
- Updates is one preferences page as well (`internal/ui/update-shell.blp`).
  It opens with the hero group (wordmark, primary action or progress, status
  line), then System Updates and Apps and Tools, then the groups
  `buildUpdatesPage` appends. Its busy banner is a toolbar top bar.
- The primary menu is at the end of the sidebar header, as in Settings and
  Files. Its first section holds Setup Assistant; its second holds
  Preferences, Keyboard Shortcuts, and About, all with mnemonics.
- Dialogs are `AdwAlertDialog` or `AdwDialog`, including About
  (`AdwAboutDialog`) and Keyboard Shortcuts (`AdwShortcutsDialog` on
  libadwaita 1.8, an `AdwDialog` of the same entries before it). Every alert
  names cancel as its default and close response.
- The standard shortcuts are registered from `internal/navigation`: Ctrl+,
  (Preferences), Ctrl+? (Shortcuts), Ctrl+Q (Quit), Ctrl+W (Close Window),
  Ctrl+R (Check for Updates), F1 (Help), F10 (primary menu), Alt+Left (back
  from Powerwash), and Alt+1… for the visible pages.

### Fixed by the audit

| Category | Finding | Now |
| --- | --- | --- |
| Responsiveness | No breakpoint, so the split view never collapsed; no minimum size | `narrow_breakpoint` collapses it below 550sp; 360×294 minimum |
| Layout | Every page nested its `AdwPreferencesPage` in a second `GtkScrolledWindow` | The page is the toolbar's content |
| Layout | Updates mounted a second preferences page and walked libadwaita's private tree to unclamp it | One page; the Updates groups are appended to it |
| Layout | Updates and avatar-chooser banners scrolled away with the content | Toolbar top bars |
| Layout | The setup flow's buttons were a bare box with a spacer under the window | An `GtkActionBar` bottom bar of a window-level `AdwToolbarView` |
| Dialogs | Deprecated `AdwAboutWindow`; hand-built shortcuts `AdwWindow` | `AdwAboutDialog`; `AdwShortcutsDialog` with an `AdwDialog` fallback |
| Dialogs | Alerts without default or close responses; Factory Reset's response a noun | All cancel by default; the response is Reset |
| Dialogs | Avatar chooser: Apply with no Cancel | Cancel and Apply replace the close button |
| Shortcuts | No Ctrl+W, no check accelerator, no back from Powerwash, no mnemonics; two dead actions | Added; `win.help` and `win.preferences` removed |
| Navigation | Collapsed Powerwash showed two back arrows leading to different places | The header's automatic back button is off on that page |
| Sidebar | Rows were boxed-list `AdwActionRow`s, taller and more padded than GNOME Settings' | `nav-row.blp` is Settings' panel row: a plain list row, 12px top and bottom, 6px sides, 12px icon gap, named by its title; sidebar width is libadwaita's default, as in Settings |
| Icons | Applications and System components shared one icon; Agents used the favorites star, Maintenance the Preferences gear, Apps a MIME type | Distinct, conventional glyphs, checked against the Adwaita catalog |
| Buttons | A destructive Uninstall on every installed row; several suggested buttons per view | One suggested action per view; destructive styling on the dialog response |
| Buttons | Icon-only row buttons not flat | Flat |
| Accessibility | Repeated Update / Trust / Trust Tap / Run buttons indistinguishable to a screen reader | Each describes its row |
| Accessibility | Unlabelled selected checks, update count, progress bar, staging spinner | Labelled |
| Accessibility | The diagnostics row and its Copy button were two focus stops for one action | One |
| Text | Group, row, and button titles in sentence case; dialog-opening buttons without "…"; "Could not" beside "Couldn't"; British spellings; external titles parsed as markup | Header Case, "…", "Couldn't", US spelling, plain-text titles |

### Open findings

| Category | Finding | Why open |
| --- | --- | --- |
| i18n | About 730 user-visible literals are not translatable; `gotext` is used in one package and never configured | A project of its own: a domain, `po/`, extraction, and an install step |
| Navigation | Powerwash is a stack sibling with its own Back button rather than an `AdwNavigationView` subpage | Changes the `navigation.Resolve` transition contract; the double-arrow defect is fixed |
| CSS | `dim-label` instead of `dimmed`; `GtkSpinner` instead of `AdwSpinner` | `dimmed` needs 1.7. `AdwSpinner` is within the floor but touches every spinner's lifecycle |
| Dialogs | Switching the release channel and Roll Back confirm only through PolicyKit | A behavior change to the privileged flows, for its own review |
| Dialogs | Closing during an update is refused with a banner rather than offered as a choice | Same |
| GSettings | Window size and maximized state are not remembered; no root schema | A new schema and its install surface |
| Text | Many single-sentence subtitles end with a period; `[DRY-RUN]` toasts are long | Style churn across ~270 strings and their tests |
| Color | Livery previews recolor SVGs with fixed Tango values | Previews are SVG files, not themed icons |
| Layout | Welcome and conclusion are custom layouts rather than `AdwStatusPage` | The wordmark is a wide picture; a status page sizes its paintable as an icon |

## Operational notes

- Re-audit with gnome-gui-spec's `skills/hig-audit/SKILL.md`. Its
  `scripts/lint-app.sh` sees only the blueprints, not the Go-built rows, so
  also grep Go for `-symbolic"` names and check them against its
  `reference/icons.md`.
- A class newer than libadwaita 1.6 in a blueprint aborts ChairLift on Bluefin
  LTS at startup; `TestBlueprintsStayWithinTheLibadwaitaFloor` holds the line.

## References

- Rationale: [ADR-0022](../adr/0022-describe-static-ui-in-blueprint.md)
- Skill: [blueprint-ui](../skills/blueprint-ui/SKILL.md)
- Verification: `make ci` (blueprint gates), `make blueprints-check`,
  `make e2e-atspi` (the behave suite drives every destination, the shortcuts
  dialog, and About through AT-SPI), `make screenshots`
