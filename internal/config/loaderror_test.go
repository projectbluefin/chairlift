package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLoadErrorErrorExactStrings(t *testing.T) {
	cause := errors.New("permission denied")

	tests := []struct {
		name string
		err  *LoadError
		want string
	}{
		{
			name: "read kind with path detail and err",
			err: &LoadError{
				Path:   "/etc/chairlift/config.yml",
				Kind:   KindRead,
				Detail: "opening config file",
				Err:    cause,
			},
			want: "config read error: /etc/chairlift/config.yml: opening config file: permission denied",
		},
		{
			name: "parse type kind with path detail and err",
			err: &LoadError{
				Path:   "/etc/chairlift/config.yml",
				Kind:   KindParseType,
				Detail: "decoding page map",
				Err:    errors.New("yaml: line 3: mapping values are not allowed in this context"),
			},
			want: "config parse/type error: /etc/chairlift/config.yml: decoding page map: yaml: line 3: mapping values are not allowed in this context",
		},
		{
			name: "schema kind with path only and nil err",
			err: &LoadError{
				Path: "/etc/chairlift/config.yml",
				Kind: KindSchema,
			},
			want: "config schema error: /etc/chairlift/config.yml",
		},
		{
			name: "schema kind with path and detail and nil err",
			err: &LoadError{
				Path:   "/etc/chairlift/config.yml",
				Kind:   KindSchema,
				Detail: "unknown key \"bogus_group\"",
			},
			want: "config schema error: /etc/chairlift/config.yml: unknown key \"bogus_group\"",
		},
		{
			name: "empty path with detail and err",
			err: &LoadError{
				Kind:   KindRead,
				Detail: "opening config file",
				Err:    cause,
			},
			want: "config read error: opening config file: permission denied",
		},
		{
			name: "empty kind",
			err: &LoadError{
				Path:   "/etc/chairlift/config.yml",
				Detail: "something went wrong",
				Err:    cause,
			},
			want: "config error: /etc/chairlift/config.yml: something went wrong: permission denied",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadErrorContainsKindLiteral(t *testing.T) {
	for _, kind := range []ErrorKind{KindRead, KindParseType, KindSchema} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			err := &LoadError{Path: "/some/path", Kind: kind, Detail: "detail"}
			if !strings.Contains(err.Error(), string(kind)) {
				t.Errorf("Error() = %q, want it to contain kind literal %q", err.Error(), string(kind))
			}
		})
	}
}

func TestLoadErrorUnwrap(t *testing.T) {
	if got := (&LoadError{Err: nil}).Unwrap(); got != nil {
		t.Errorf("Unwrap() with nil Err = %v, want nil", got)
	}

	cause := errors.New("boom")
	if got := (&LoadError{Err: cause}).Unwrap(); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
}

func TestLoadErrorErrorsIs(t *testing.T) {
	readErr := &LoadError{Kind: KindRead, Err: os.ErrNotExist}
	if !errors.Is(readErr, os.ErrNotExist) {
		t.Errorf("errors.Is(readErr, os.ErrNotExist) = false, want true")
	}

	schemaErr := &LoadError{Kind: KindSchema}
	if errors.Is(schemaErr, os.ErrNotExist) {
		t.Errorf("errors.Is(schemaErr, os.ErrNotExist) = true, want false")
	}
}

func TestLoadErrorErrorsAs(t *testing.T) {
	original := &LoadError{Path: "/etc/chairlift/config.yml", Kind: KindSchema, Detail: "unknown key"}
	wrapped := fmt.Errorf("wrapped: %w", original)

	var target *LoadError
	if !errors.As(wrapped, &target) {
		t.Fatalf("errors.As(wrapped, &target) = false, want true")
	}
	if target.Kind != original.Kind {
		t.Errorf("target.Kind = %q, want %q", target.Kind, original.Kind)
	}
}

// TestParseErrorNamesItsCauseOnce holds issue #348: a parser or decoder
// failure keeps the cause's own text as Detail (for its line attribution) and
// the cause itself as Err (for errors.Is/As). The rendered diagnostic — and so
// the persistent toast and the CONFIGURATION ERROR log — must state it once.
func TestParseErrorNamesItsCauseOnce(t *testing.T) {
	const path = "/etc/chairlift/config.yml"
	for name, data := range map[string]string{
		"unterminated flow mapping": "applications_page:\n  brew_group: {enabled: true\n",
		"wrong value type":          "applications_page:\n  brew_group:\n    enabled: [1]\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseAndValidate(configSourceForPath(path), []byte(data))
			if err == nil || err.Err == nil {
				t.Fatalf("parseAndValidate(%q) error = %+v, want a failure carrying its cause", data, err)
			}
			cause := err.Err.Error()
			for _, rendered := range []string{err.Error(), err.ToastMessage()} {
				if got := strings.Count(rendered, cause); got != 1 {
					t.Errorf("%q states its cause %q %d times, want once", rendered, cause, got)
				}
			}
		})
	}

	// A detail that only gives context still renders the cause after it.
	contextOnly := &LoadError{Path: path, Kind: KindRead, Detail: "reading configuration file", Err: os.ErrPermission}
	if got := contextOnly.Error(); !strings.HasSuffix(got, ": reading configuration file: "+os.ErrPermission.Error()) {
		t.Errorf("Error() = %q, want the context detail followed by the cause", got)
	}
}
