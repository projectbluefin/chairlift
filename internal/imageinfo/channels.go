package imageinfo

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SystemTablePaths are the only locations a channel-table override is read
// from, in precedence order: the administrator-owned file first, then the
// package-maintainer default shipped with the image.
//
// This list is deliberately narrower than internal/config's, which also
// searches the working directory. The channel table decides the image
// reference the *privileged* helper hands to `bootc switch`, so a table
// sourced from a user-writable location would let any local user redirect a
// PolicyKit-authenticated system switch at an image of their choosing. Both
// paths here are root-owned by construction, matching the repository's
// invariant that the privileged path is never configurable from
// ChairLift's user-writable configuration.
var SystemTablePaths = []string{
	"/etc/chairlift/channels.yml",
	"/usr/share/chairlift/channels.yml",
}

// rawTable is the YAML shape of a channel-table file:
//
//	images:
//	  ghcr.io/tuna-os/tromso:
//	    stable_tags:  [latest, stable]
//	    testing_tags: [testing]
//	    to_testing:
//	      latest: testing
//	      stable: testing
//	    to_stable:
//	      testing: stable
//
// An entry whose key matches a built-in image replaces that built-in
// outright rather than merging into it, so an override can remove a mapping
// as well as add one. Any other key is added.
type rawTable struct {
	Images map[string]rawImageChannels `yaml:"images"`
	// Drivers overrides the graphics-driver variant table in the same file,
	// because it answers the same question — which image references exist —
	// and the privileged helper resolves a driver switch through it exactly
	// as it resolves a channel switch. A second file would be a second
	// chance for the GUI and the helper to disagree.
	//
	//	drivers:
	//	  ghcr.io/tuna-os/tromso:
	//	    standard: [latest, stable]
	//	    nvidia:   [latest]
	Drivers map[string]map[string][]string `yaml:"drivers"`
}

type rawImageChannels struct {
	StableTags  []string          `yaml:"stable_tags"`
	TestingTags []string          `yaml:"testing_tags"`
	ToTesting   map[string]string `yaml:"to_testing"`
	ToStable    map[string]string `yaml:"to_stable"`
}

// activeTable is the channel table every lookup resolves through. It starts
// as the built-in table and is replaced wholesale by LoadSystemTable when an
// override file is present.
var activeTable = builtinTable()

// activeDriverTable is the graphics-driver variant table every lookup
// resolves through, on the same terms as activeTable.
var activeDriverTable = builtinDriverTable()

// systemTableError is non-empty when the most recent LoadTable call could
// not apply a channel-table override. The views read it through
// SystemTableError and fail closed: an override that exists but cannot be
// applied must disable the rebase controls it cannot resolve, rather than
// silently falling back to the built-in table and offering a switch the
// privileged helper would then refuse after authentication.
var systemTableError string

// SystemTableError returns why the channel-table override could not be
// applied, or "" when the last load applied cleanly — including the ordinary
// case of no override file at all, which keeps the compiled-in table
// authoritative and the rebase controls usable.
func SystemTableError() string {
	return systemTableError
}

// builtinDriverTable returns a fresh copy of the compiled-in driver table.
func builtinDriverTable() map[string][]driverStreams {
	result := make(map[string][]driverStreams, len(imageDriverMap))
	for ref, entries := range imageDriverMap {
		cloned := make([]driverStreams, 0, len(entries))
		for _, entry := range entries {
			cloned = append(cloned, driverStreams{
				driver:  entry.driver,
				streams: append([]string(nil), entry.streams...),
			})
		}
		result[ref] = cloned
	}
	return result
}

// builtinTable returns a fresh copy of the compiled-in channel table, so a
// caller (or an override merge) cannot mutate the package's own definition.
func builtinTable() map[string]imageChannels {
	result := make(map[string]imageChannels, len(imageChannelMap))
	for ref, channels := range imageChannelMap {
		result[ref] = channels.clone()
	}
	return result
}

func (c imageChannels) clone() imageChannels {
	out := imageChannels{
		stableTags:  append([]string(nil), c.stableTags...),
		testingTags: append([]string(nil), c.testingTags...),
		toTesting:   make(map[string]string, len(c.toTesting)),
		toStable:    make(map[string]string, len(c.toStable)),
	}
	for from, to := range c.toTesting {
		out.toTesting[from] = to
	}
	for from, to := range c.toStable {
		out.toStable[from] = to
	}
	return out
}

// KnownImages returns the registry paths the active channel table covers, in
// sorted order. It exists so the Help/diagnostics surface — and the tests —
// can show which images ChairLift can switch channels for without reaching
// into package internals.
func KnownImages() []string {
	refs := make([]string, 0, len(activeTable))
	for ref := range activeTable {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

// ParseTable decodes and validates a channel-table override, returning the
// complete table to use: the built-in entries with the file's entries
// overlaid. It rejects a file that would produce an unusable mapping rather
// than silently dropping the bad entry, because a half-applied table is
// exactly the situation that produces a wrong `bootc switch` target.
func ParseTable(reader io.Reader) (map[string]imageChannels, error) {
	table, _, err := parseTables(reader)
	return table, err
}

// parseTables decodes both tables the file can carry.
func parseTables(reader io.Reader) (map[string]imageChannels, map[string][]driverStreams, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, fmt.Errorf("reading channel table: %w", err)
	}

	var raw rawTable
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true) // a typo'd key is an error, not a silent no-op
	if err := decoder.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("parsing channel table: %w", err)
	}

	table := builtinTable()
	for ref, entry := range raw.Images {
		channels, err := convertEntry(ref, entry)
		if err != nil {
			return nil, nil, err
		}
		table[ref] = channels
	}

	drivers := builtinDriverTable()
	for ref, entry := range raw.Drivers {
		converted, err := convertDriverEntry(ref, entry)
		if err != nil {
			return nil, nil, err
		}
		drivers[ref] = converted
	}

	return table, drivers, nil
}

// convertDriverEntry validates one `drivers:` entry. The rules mirror the
// built-in table's own invariants: the key is a base image path, every
// flavour is one ChairLift can name, and the standard flavour must be
// present — a host on the base image that could not switch back to it would
// be stranded on a driver image.
func convertDriverEntry(ref string, entry map[string][]string) ([]driverStreams, error) {
	if hasTag(ref) {
		return nil, fmt.Errorf("driver table key %q must be a registry path without a tag", ref)
	}
	if !strings.Contains(ref, "/") {
		return nil, fmt.Errorf("driver table key %q must be a full registry path (registry/org/image)", ref)
	}
	for _, driver := range knownDrivers {
		if strings.HasSuffix(ref, driver.suffix()) {
			return nil, fmt.Errorf("driver table key %q must be the base image, not the %s variant", ref, driver)
		}
	}

	// Table order decides the order the UI offers flavours in, so it is
	// fixed here rather than taken from YAML map iteration.
	order := []Driver{DriverStandard, DriverNVIDIA, DriverNVIDIAOpen}
	known := make(map[string]bool, len(order))
	for _, driver := range order {
		known[string(driver)] = true
	}
	for name := range entry {
		if !known[name] {
			return nil, fmt.Errorf("driver table entry %q: unknown driver %q", ref, name)
		}
	}
	standardStreams := entry[string(DriverStandard)]
	if len(standardStreams) == 0 {
		return nil, fmt.Errorf("driver table entry %q needs a %s stream list, otherwise a host on a driver image could not switch back", ref, DriverStandard)
	}

	converted := make([]driverStreams, 0, len(entry))
	for _, driver := range order {
		streams, ok := entry[string(driver)]
		if !ok {
			continue
		}
		if len(streams) == 0 {
			return nil, fmt.Errorf("driver table entry %q: driver %q has an empty stream list", ref, driver)
		}
		if driver != DriverStandard {
			for _, stream := range streams {
				if !contains(standardStreams, stream) {
					return nil, fmt.Errorf("driver table entry %q: driver %q stream %q is not in the standard stream list, so a host could not switch back", ref, driver, stream)
				}
			}
		}
		converted = append(converted, driverStreams{
			driver:  driver,
			streams: append([]string(nil), streams...),
		})
	}
	return converted, nil
}

// convertEntry validates one override entry and converts it to the internal
// form. The rules are the same invariants the built-in table's own test
// asserts: every mapping endpoint must be a tag the same image declares, and
// every testing tag must have a way back to stable — otherwise a host that
// took the switch could not undo it.
func convertEntry(ref string, entry rawImageChannels) (imageChannels, error) {
	if ref == "" {
		return imageChannels{}, fmt.Errorf("channel table has an entry with an empty image reference")
	}
	if hasTag(ref) {
		return imageChannels{}, fmt.Errorf("channel table key %q must be a registry path without a tag", ref)
	}
	if !strings.Contains(ref, "/") {
		return imageChannels{}, fmt.Errorf("channel table key %q must be a full registry path (registry/org/image)", ref)
	}
	if len(entry.TestingTags) == 0 || len(entry.StableTags) == 0 {
		return imageChannels{}, fmt.Errorf("channel table entry %q needs both stable_tags and testing_tags", ref)
	}

	channels := imageChannels{
		stableTags:  append([]string(nil), entry.StableTags...),
		testingTags: append([]string(nil), entry.TestingTags...),
		toTesting:   make(map[string]string, len(entry.ToTesting)),
		toStable:    make(map[string]string, len(entry.ToStable)),
	}

	for from, to := range entry.ToTesting {
		if !contains(channels.stableTags, from) {
			return imageChannels{}, fmt.Errorf("channel table entry %q: to_testing source %q is not in stable_tags", ref, from)
		}
		if !contains(channels.testingTags, to) {
			return imageChannels{}, fmt.Errorf("channel table entry %q: to_testing target %q is not in testing_tags", ref, to)
		}
		channels.toTesting[from] = to
	}

	for from, to := range entry.ToStable {
		if !contains(channels.testingTags, from) {
			return imageChannels{}, fmt.Errorf("channel table entry %q: to_stable source %q is not in testing_tags", ref, from)
		}
		if !contains(channels.stableTags, to) {
			return imageChannels{}, fmt.Errorf("channel table entry %q: to_stable target %q is not in stable_tags", ref, to)
		}
		channels.toStable[from] = to
	}

	for _, tag := range channels.testingTags {
		if _, ok := channels.toStable[tag]; !ok {
			return imageChannels{}, fmt.Errorf("channel table entry %q: testing tag %q has no to_stable mapping, so a host could not switch back", ref, tag)
		}
	}

	return channels, nil
}

// LoadTable reads and applies the first existing channel-table file in
// paths. It returns the path it applied, or "" when no candidate exists —
// which is the ordinary case, and leaves the built-in table active. A
// candidate that exists but is invalid is an error and leaves the previously
// active table untouched: a broken override must not silently degrade into
// "no images are switchable", nor into a partially applied mapping.
// loadTable is LoadTable's implementation. It is split out so the public
// LoadTable can record whether the load succeeded for the views' fail-closed
// behavior while keeping the return values identical for existing callers.
func loadTable(paths []string) (string, error) {
	for _, path := range paths {
		file, err := os.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("opening channel table %s: %w", path, err)
		}

		table, drivers, parseErr := parseTables(file)
		closeErr := file.Close()
		if parseErr != nil {
			return "", fmt.Errorf("%s: %w", path, parseErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("closing channel table %s: %w", path, closeErr)
		}

		activeTable = table
		activeDriverTable = drivers
		return path, nil
	}
	return "", nil
}

// LoadTable reads and applies the first existing channel-table file in
// paths. It returns the path it applied, or "" when no candidate exists —
// which is the ordinary case, and leaves the built-in table active. A
// candidate that exists but is invalid is an error and leaves the previously
// active table untouched: a broken override must not silently degrade into
// "no images are switchable", nor into a partially applied mapping.
//
// Every call also refreshes systemTableError so the views know whether the
// authoritative table applied cleanly.
func LoadTable(paths []string) (string, error) {
	path, err := loadTable(paths)
	systemTableError = ""
	if err != nil {
		systemTableError = fmt.Sprintf("%v", err)
	}
	return path, err
}

// LoadSystemTable applies the channel-table override from the fixed system
// paths. Both the GUI and the privileged helper call it at startup, so they
// always resolve the same table — a helper that used the built-in table
// while the GUI used an override would offer a switch and then refuse it.
func LoadSystemTable() (string, error) {
	return LoadTable(SystemTablePaths)
}

// ResetTable restores the built-in tables. It exists for tests.
func ResetTable() {
	activeTable = builtinTable()
	activeDriverTable = builtinDriverTable()
	systemTableError = ""
}
