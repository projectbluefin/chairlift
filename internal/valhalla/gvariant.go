package valhalla

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// parseStrv accepts GVariant's string-array syntax, not shell quoting. In
// particular, commas inside strings are data, and a quoted apostrophe is \'
// rather than the shell-only '\” construction. Invalid input must not be
// replaced with an empty array: that would silently discard user preferences.
func parseStrv(raw string) ([]string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "@as") {
		s = strings.TrimSpace(s[3:])
	}
	if len(s) < 2 || s[0] != '[' {
		return nil, errors.New("expected GVariant string array")
	}
	s = strings.TrimSpace(s[1:])
	var result []string
	if s == "]" {
		return result, nil
	}
	for {
		value, rest, err := parseString(s)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
		s = strings.TrimSpace(rest)
		if s == "]" {
			return result, nil
		}
		if len(s) == 0 || s[0] != ',' {
			return nil, errors.New("expected comma or closing array bracket")
		}
		s = strings.TrimSpace(s[1:])
		// GVariant allows a trailing comma.
		if s == "]" {
			return result, nil
		}
	}
}

func parseString(s string) (string, string, error) {
	if len(s) < 2 || (s[0] != '\'' && s[0] != '"') {
		return "", "", errors.New("expected quoted GVariant string")
	}
	quote := s[0]
	var value strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == quote {
			text := value.String()
			if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return "", "", errors.New("invalid GVariant string encoding")
			}
			return text, s[i+1:], nil
		}
		if c != '\\' {
			if c < 0x20 {
				return "", "", errors.New("unescaped control character in GVariant string")
			}
			value.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '\\', '\'', '"':
			value.WriteByte(s[i])
		case 'a':
			value.WriteByte('\a')
		case 'b':
			value.WriteByte('\b')
		case 'f':
			value.WriteByte('\f')
		case 'n':
			value.WriteByte('\n')
		case 'r':
			value.WriteByte('\r')
		case 't':
			value.WriteByte('\t')
		case 'v':
			value.WriteByte('\v')
		case 'u', 'U':
			n := 4
			if s[i] == 'U' {
				n = 8
			}
			if i+n >= len(s) {
				return "", "", errors.New("incomplete GVariant Unicode escape")
			}
			r, err := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
			if err != nil || r == 0 || r > utf8.MaxRune || !utf8.ValidRune(rune(r)) {
				return "", "", errors.New("invalid GVariant Unicode escape")
			}
			value.WriteRune(rune(r))
			i += n
		default:
			return "", "", fmt.Errorf("invalid GVariant escape \\%c", s[i])
		}
	}
	return "", "", errors.New("unterminated GVariant string")
}

func formatStrv(paths []string) string {
	quoted := make([]string, len(paths))
	for i, path := range paths {
		var value strings.Builder
		value.WriteByte('\'')
		for _, r := range path {
			switch r {
			case '\\':
				value.WriteString(`\\`)
			case '\'':
				value.WriteString(`\'`)
			case '\a':
				value.WriteString(`\a`)
			case '\b':
				value.WriteString(`\b`)
			case '\f':
				value.WriteString(`\f`)
			case '\n':
				value.WriteString(`\n`)
			case '\r':
				value.WriteString(`\r`)
			case '\t':
				value.WriteString(`\t`)
			case '\v':
				value.WriteString(`\v`)
			default:
				value.WriteRune(r)
			}
		}
		value.WriteByte('\'')
		quoted[i] = value.String()
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
