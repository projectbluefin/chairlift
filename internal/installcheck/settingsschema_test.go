package installcheck

import (
	"encoding/xml"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// internal/settings is the adapter between the shipped `updates` GSettings
// schema and userprefs.Values. Nothing held the two sides together.
//
// The failure mode is not a compile error and not a wrong value — it is an
// abort. g_settings_get_boolean() treats a key the schema does not declare as
// programmer error: GLib logs "Model does not have key" at G_LOG_LEVEL_ERROR
// and calls g_assert_not_reached(), so renaming a key in
// data/io.projectbluefin.chairlift.updates.gschema.xml without renaming the
// matching constant in internal/settings/store.go kills ChairLift the moment
// the Updates page reads preferences. The reverse direction is quieter and
// worse: a key added to the schema that Values() never reads is a preference
// the user can toggle in dconf while Update All keeps ignoring it.
//
// The fallback defaults are a third, entirely silent divergence. Values()
// returns hardcoded literals when the schema is not installed, so a host
// without the schema and a host with it must agree; today they do, and only
// by hand.
//
// None of this can be tested from internal/settings itself: that package
// imports puregotk's gio and gobject, so a _test.go beside store.go builds a
// test binary that panics during package init on a headless runner
// (docs/agents/skills/gtk-headless-tests.md). This gate therefore does what
// TestDestructiveActionsRequireConfirmation does for internal/views — reads
// the source with go/parser and the schema with encoding/xml, and holds them
// to each other without loading either.

const (
	settingsStoreSource = "internal/settings/store.go"
	updatesSchemaFile   = "data/io.projectbluefin.chairlift.updates.gschema.xml"
)

// gschemaList mirrors the subset of a GSettings schema XML file these tests
// read. Fields the schema declares but nothing here names (summary,
// description, range, path) are ignored by encoding/xml.
type gschemaList struct {
	Schemas []gschemaSchema `xml:"schema"`
}

type gschemaSchema struct {
	ID   string       `xml:"id,attr"`
	Keys []gschemaKey `xml:"key"`
}

type gschemaKey struct {
	Name    string `xml:"name,attr"`
	Type    string `xml:"type,attr"`
	Default string `xml:"default"`
}

// settingsContract is internal/settings/store.go as this gate reads it: the
// schema it names, and for each userprefs.Values field the GSettings key
// Values() reads it from and the literal Values() falls back to when the
// schema is absent.
type settingsContract struct {
	schemaID  string
	keys      map[string]string // Values field name -> GSettings key name
	fallbacks map[string]bool   // Values field name -> literal returned with no schema
}

// readSettingsContract parses internal/settings/store.go and recovers the
// contract above. It never imports the package, so it stays headless.
func readSettingsContract(t *testing.T) settingsContract {
	t.Helper()

	path := filepath.Join(RepoRoot(), settingsStoreSource)
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", settingsStoreSource, err)
	}

	consts := stringConstants(t, file)
	schemaID, ok := consts["SchemaID"]
	if !ok {
		t.Fatalf("%s declares no SchemaID string constant", settingsStoreSource)
	}

	values := methodNamed(file, "Values")
	if values == nil {
		t.Fatalf("%s declares no Values method: this gate would be vacuous", settingsStoreSource)
	}

	fallbackLit, liveLit := returnedComposites(t, values)
	contract := settingsContract{
		schemaID:  schemaID,
		keys:      map[string]string{},
		fallbacks: map[string]bool{},
	}

	for field, expr := range compositeFields(t, liveLit) {
		name := getBooleanKeyArgument(expr)
		if name == "" {
			t.Errorf("Values() field %s is not a settings.GetBoolean(<key constant>) call: this gate cannot see which key it reads", field)
			continue
		}
		key, ok := consts[name]
		if !ok {
			t.Errorf("Values() field %s reads key constant %s, which is not a string constant in %s", field, name, settingsStoreSource)
			continue
		}
		contract.keys[field] = key
	}

	for field, expr := range compositeFields(t, fallbackLit) {
		ident, ok := expr.(*ast.Ident)
		if !ok || (ident.Name != "true" && ident.Name != "false") {
			t.Errorf("Values() no-schema fallback for %s is not a bool literal: this gate cannot compare it with the schema default", field)
			continue
		}
		contract.fallbacks[field] = ident.Name == "true"
	}

	if len(contract.keys) == 0 {
		t.Fatal("Values() reads no GSettings keys: this gate would be vacuous")
	}
	return contract
}

// stringConstants returns every top-level `const name = "literal"` in the
// file, keyed by constant name.
func stringConstants(t *testing.T, file *ast.File) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if i >= len(value.Values) {
					continue
				}
				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquoting %s = %s: %v", name.Name, lit.Value, err)
				}
				out[name.Name] = unquoted
			}
		}
	}
	return out
}

// methodNamed returns the method with the given name, or nil.
func methodNamed(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv != nil && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	return nil
}

// returnedComposites splits Values() into its two composite literals: the one
// returned from inside the no-schema guard, and the one returned at the end.
func returnedComposites(t *testing.T, fn *ast.FuncDecl) (fallback, live *ast.CompositeLit) {
	t.Helper()

	for _, stmt := range fn.Body.List {
		switch typed := stmt.(type) {
		case *ast.IfStmt:
			if lit := firstReturnedComposite(typed.Body); lit != nil && fallback == nil {
				fallback = lit
			}
		case *ast.ReturnStmt:
			if lit := returnedComposite(typed); lit != nil {
				live = lit
			}
		}
	}

	if fallback == nil {
		t.Fatal("Values() has no composite literal returned from a guard: the no-schema fallback is unreadable to this gate")
	}
	if live == nil {
		t.Fatal("Values() has no composite literal returned at the end: the schema-backed result is unreadable to this gate")
	}
	return fallback, live
}

func firstReturnedComposite(block *ast.BlockStmt) *ast.CompositeLit {
	if block == nil {
		return nil
	}
	for _, stmt := range block.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			if lit := returnedComposite(ret); lit != nil {
				return lit
			}
		}
	}
	return nil
}

func returnedComposite(ret *ast.ReturnStmt) *ast.CompositeLit {
	if len(ret.Results) != 1 {
		return nil
	}
	lit, _ := ret.Results[0].(*ast.CompositeLit)
	return lit
}

// compositeFields returns the literal's `Field: expr` elements keyed by field
// name. A literal with positional elements would silently yield nothing, so
// that case fails rather than passing vacuously.
func compositeFields(t *testing.T, lit *ast.CompositeLit) map[string]ast.Expr {
	t.Helper()

	out := map[string]ast.Expr{}
	for _, element := range lit.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("userprefs.Values literal at offset %d uses positional fields: this gate reads named fields only", lit.Pos())
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			continue
		}
		out[key.Name] = pair.Value
	}
	return out
}

// getBooleanKeyArgument returns the identifier passed to a
// `<receiver>.GetBoolean(<ident>)` call, or "" for anything else.
func getBooleanKeyArgument(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return ""
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "GetBoolean" {
		return ""
	}
	ident, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// readUpdatesSchema returns the single schema declared by the shipped updates
// gschema, keyed by key name.
func readUpdatesSchema(t *testing.T) (id string, keys map[string]gschemaKey) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(RepoRoot(), updatesSchemaFile))
	if err != nil {
		t.Fatalf("reading %s: %v", updatesSchemaFile, err)
	}

	var list gschemaList
	if err := xml.Unmarshal(raw, &list); err != nil {
		t.Fatalf("parsing %s: %v", updatesSchemaFile, err)
	}
	if len(list.Schemas) != 1 {
		t.Fatalf("%s declares %d schemas, want exactly 1", updatesSchemaFile, len(list.Schemas))
	}

	schema := list.Schemas[0]
	if len(schema.Keys) == 0 {
		t.Fatalf("%s declares no keys: this gate would be vacuous", updatesSchemaFile)
	}

	keys = make(map[string]gschemaKey, len(schema.Keys))
	for _, key := range schema.Keys {
		if _, duplicate := keys[key.Name]; duplicate {
			t.Errorf("%s declares key %q twice", updatesSchemaFile, key.Name)
		}
		keys[key.Name] = key
	}
	return schema.ID, keys
}

// TestSettingsSchemaIDMatchesTheShippedSchema keeps settings.SchemaID pointing
// at the schema the repository actually installs. A mismatch is not a build
// failure: gio.NewSettings() on an unregistered id aborts the process, and
// SchemaAvailable() returning false first only converts that into every
// preference silently reverting to the hardcoded fallback.
func TestSettingsSchemaIDMatchesTheShippedSchema(t *testing.T) {
	contract := readSettingsContract(t)
	id, _ := readUpdatesSchema(t)

	if contract.schemaID != id {
		t.Errorf("settings.SchemaID = %q, %s declares id %q", contract.schemaID, updatesSchemaFile, id)
	}
}

// TestSettingsReadsEveryKeyTheUpdatesSchemaDeclares holds both sides of the
// key inventory to each other, so neither can gain or rename a key alone.
func TestSettingsReadsEveryKeyTheUpdatesSchemaDeclares(t *testing.T) {
	contract := readSettingsContract(t)
	_, declared := readUpdatesSchema(t)

	read := map[string]string{} // key name -> Values field reading it
	for field, key := range contract.keys {
		if other, duplicate := read[key]; duplicate {
			t.Errorf("Values() reads key %q for both %s and %s", key, other, field)
		}
		read[key] = field

		if _, ok := declared[key]; !ok {
			t.Errorf("Values() field %s reads key %q, which %s does not declare: g_settings_get_boolean aborts on an undeclared key", field, key, updatesSchemaFile)
		}
	}

	for _, key := range sortedKeys(declared) {
		if _, ok := read[key]; !ok {
			t.Errorf("%s declares key %q that Values() never reads: the preference is settable and ignored", updatesSchemaFile, key)
		}
	}
}

// TestSettingsKeysAreBooleansInTheUpdatesSchema guards the type as well as the
// name: Values() reads every key with GetBoolean, which aborts just the same
// on a key the schema declares with another type.
func TestSettingsKeysAreBooleansInTheUpdatesSchema(t *testing.T) {
	contract := readSettingsContract(t)
	_, declared := readUpdatesSchema(t)

	for _, field := range sortedKeys(contract.keys) {
		key := contract.keys[field]
		entry, ok := declared[key]
		if !ok {
			continue // reported by TestSettingsReadsEveryKeyTheUpdatesSchemaDeclares
		}
		if entry.Type != "b" {
			t.Errorf("Values() reads key %q with GetBoolean, but %s declares it type=%q", key, updatesSchemaFile, entry.Type)
		}
	}
}

// TestSettingsFallbackDefaultsMatchTheShippedSchema is the silent one. A host
// without the schema installed takes the hardcoded literals in Values(); a
// host with it takes the schema defaults. Divergence means Update All includes
// a source on one host and skips it on the other, with nothing logged and no
// test failing.
func TestSettingsFallbackDefaultsMatchTheShippedSchema(t *testing.T) {
	contract := readSettingsContract(t)
	_, declared := readUpdatesSchema(t)

	for _, field := range sortedKeys(contract.keys) {
		key := contract.keys[field]
		entry, ok := declared[key]
		if !ok {
			continue // reported by TestSettingsReadsEveryKeyTheUpdatesSchemaDeclares
		}

		fallback, ok := contract.fallbacks[field]
		if !ok {
			t.Errorf("Values() reads key %q for %s but returns no no-schema fallback for that field", key, field)
			continue
		}

		want, err := strconv.ParseBool(entry.Default)
		if err != nil {
			t.Errorf("%s declares key %q with default %q, which is not a boolean: %v", updatesSchemaFile, key, entry.Default, err)
			continue
		}
		if fallback != want {
			t.Errorf("Values() falls back to %s=%t with no schema, but %s defaults key %q to %t", field, fallback, updatesSchemaFile, key, want)
		}
	}

	for _, field := range sortedKeys(contract.fallbacks) {
		if _, ok := contract.keys[field]; !ok {
			t.Errorf("Values() returns a no-schema fallback for %s that no GSettings key backs: the field is unreachable from the schema", field)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
