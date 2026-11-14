package installcheck

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/printerapp"
)

// The Printers group saves each family's USB printer through the `gsettings`
// tool against data/io.projectbluefin.chairlift.printers.gschema.xml. Nothing
// checks that binding at compile time: a key renamed on one side makes every
// choice fail to save, and a type changed on one side makes `gsettings set`
// reject the dictionary the store writes. These gates hold the two together.

const printersSchemaFile = "data/io.projectbluefin.chairlift.printers.gschema.xml"

func TestPrintersSchemaIDMatchesTheShippedSchema(t *testing.T) {
	id, _ := readGSchema(t, printersSchemaFile)
	if id != printerapp.USBSchemaID {
		t.Errorf("printerapp.USBSchemaID = %q, %s declares id %q", printerapp.USBSchemaID, printersSchemaFile, id)
	}
}

// TestPrintersSchemaDeclaresEveryKey holds printerapp.USBKeys and the
// schema's key inventory to each other in both directions, and requires the
// string-to-string dictionary the store reads and writes, empty by default
// so a fresh account passes no printer through.
func TestPrintersSchemaDeclaresEveryKey(t *testing.T) {
	_, declared := readGSchema(t, printersSchemaFile)

	known := map[string]bool{}
	for _, key := range printerapp.USBKeys {
		known[key] = true
		gkey, ok := declared[key]
		if !ok {
			t.Errorf("printerapp.USBKeys names %q, which %s does not declare", key, printersSchemaFile)
			continue
		}
		if gkey.Type != "a{ss}" {
			t.Errorf("%s declares %q with type %q; the store writes an a{ss} dictionary", printersSchemaFile, key, gkey.Type)
		}
		if got := strings.TrimSpace(gkey.Default); got != "{}" {
			t.Errorf("%s default for %q = %q, want an empty dictionary", printersSchemaFile, key, got)
		}
	}
	for _, key := range sortedKeys(declared) {
		if !known[key] {
			t.Errorf("%s declares key %q that printerapp.USBKeys does not name", printersSchemaFile, key)
		}
	}
}
