package casing_test

import (
	"testing"

	"github.com/pmarschik/kongfig/casing"
)

// Table-driven split-words boundary cases used across many converters.
var splitWordsCases = []struct {
	in   string
	want string // kebab form — easy to read
}{
	// Simple CamelCase
	{"LogLevel", "log-level"},
	{"Port", "port"},
	{"MyField", "my-field"},
	// Pure acronyms
	{"ID", "id"},
	{"URL", "url"},
	{"DB", "db"},
	// Acronym followed by word
	{"DBConfig", "db-config"},
	{"APIKey", "api-key"},
	{"HTTPSPort", "https-port"},
	// Digit boundaries
	{"V2", "v2"},
	{"OAuth2Token", "o-auth2-token"},
	// Already lower — no word boundaries → single word
	{"port", "port"},
	// Empty string
	{"", ""},
	// Single uppercase
	{"A", "a"},
	// Fully upper → single word
	{"HTTP", "http"},
}

func TestLowerKebab(t *testing.T) {
	for _, c := range splitWordsCases {
		got := casing.LowerKebab(c.in)
		if got != c.want {
			t.Errorf("LowerKebab(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestKebabCase_IsAliasOfLowerKebab(t *testing.T) {
	inputs := []string{"LogLevel", "APIKey", "DBConfig", "Port", ""}
	for _, s := range inputs {
		if casing.KebabCase(s) != casing.LowerKebab(s) {
			t.Errorf("KebabCase(%q) != LowerKebab(%q)", s, s)
		}
	}
}

func TestUpperKebab(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LogLevel", "LOG-LEVEL"},
		{"APIKey", "API-KEY"},
		{"DBConfig", "DB-CONFIG"},
		{"Port", "PORT"},
		{"ID", "ID"},
		{"", ""},
	}
	for _, c := range cases {
		got := casing.UpperKebab(c.in)
		if got != c.want {
			t.Errorf("UpperKebab(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLowerSnake(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LogLevel", "log_level"},
		{"DBConfig", "db_config"},
		{"APIKey", "api_key"},
		{"Port", "port"},
		{"ID", "id"},
		{"", ""},
	}
	for _, c := range cases {
		got := casing.LowerSnake(c.in)
		if got != c.want {
			t.Errorf("LowerSnake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSnakeCase_IsAliasOfLowerSnake(t *testing.T) {
	inputs := []string{"LogLevel", "APIKey", "DBConfig", "Port", ""}
	for _, s := range inputs {
		if casing.SnakeCase(s) != casing.LowerSnake(s) {
			t.Errorf("SnakeCase(%q) != LowerSnake(%q)", s, s)
		}
	}
}

func TestUpperSnake(t *testing.T) {
	cases := []struct{ in, want string }{
		{"LogLevel", "LOG_LEVEL"},
		{"APIKey", "API_KEY"},
		{"DBConfig", "DB_CONFIG"},
		{"Port", "PORT"},
		{"ID", "ID"},
		{"", ""},
	}
	for _, c := range cases {
		got := casing.UpperSnake(c.in)
		if got != c.want {
			t.Errorf("UpperSnake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPascalCase(t *testing.T) {
	cases := []struct{ in, want string }{
		// Normalises acronyms: splits first, then title-cases each word.
		{"LogLevel", "LogLevel"},
		{"APIKey", "ApiKey"},
		{"DBConfig", "DbConfig"},
		{"Port", "Port"},
		{"ID", "Id"},
		{"URL", "Url"},
		{"", ""},
	}
	for _, c := range cases {
		got := casing.PascalCase(c.in)
		if got != c.want {
			t.Errorf("PascalCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCamelCase(t *testing.T) {
	cases := []struct{ in, want string }{
		// First word lowercase, rest title-cased.
		{"LogLevel", "logLevel"},
		{"APIKey", "apiKey"},
		{"DBConfig", "dbConfig"},
		{"Port", "port"},
		{"ID", "id"},
		// Already lower single word — unchanged.
		{"port", "port"},
		// Empty — falls back to returning the empty input.
		{"", ""},
	}
	for _, c := range cases {
		got := casing.CamelCase(c.in)
		if got != c.want {
			t.Errorf("CamelCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAsIs(t *testing.T) {
	inputs := []string{"LogLevel", "log-level", "LOG_LEVEL", "Port", ""}
	for _, s := range inputs {
		if got := casing.AsIs(s); got != s {
			t.Errorf("AsIs(%q) = %q, want %q", s, got, s)
		}
	}
}

// TestSplitWordsBoundaries verifies the boundary detection through LowerKebab
// for tricky patterns not covered by the main table.
func TestSplitWordsBoundaries(t *testing.T) {
	cases := []struct{ in, want string }{
		// Digit after uppercase — "V2" splits as ["v2"] (no boundary before digit)
		{"V2", "v2"},
		// Consecutive uppercase ending with lowercase — last pair splits.
		{"HTTPSEnabled", "https-enabled"},
		// Transition from lower to upper.
		{"fooBar", "foo-bar"},
		// Single lowercase — no split.
		{"x", "x"},
		// Multiple consecutive uppers followed by lowercase — split before last upper.
		{"XMLParser", "xml-parser"},
	}
	for _, c := range cases {
		got := casing.LowerKebab(c.in)
		if got != c.want {
			t.Errorf("boundary(%q) LowerKebab = %q, want %q", c.in, got, c.want)
		}
	}
}
