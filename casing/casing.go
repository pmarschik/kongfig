// Package casing provides name mapper functions for converting Go CamelCase
// field names to various naming conventions used in configuration keys.
package casing

import (
	"strings"
	"unicode"
)

// NameMapper converts a Go struct field name to a kongfig key segment.
// It is used when a kongfig struct tag has no explicit name.
// This is the default implementation used by [schema.DefaultNameMapper].
type NameMapper func(fieldName string) string

// KebabCase converts a CamelCase Go field name to kebab-case.
// Alias for [LowerKebab].
//
//	LogLevel → log-level
//	DBConfig → db-config
//	APIKey   → api-key
func KebabCase(s string) string { return LowerKebab(s) }

// SnakeCase converts a CamelCase Go field name to snake_case.
// Alias for [LowerSnake].
//
//	LogLevel → log_level
//	DBConfig → db_config
func SnakeCase(s string) string { return LowerSnake(s) }

// LowerKebab converts a CamelCase Go field name to lower-kebab-case.
//
//	LogLevel → log-level, APIKey → api-key
func LowerKebab(s string) string { return strings.Join(splitWords(s), "-") }

// UpperKebab converts a CamelCase Go field name to UPPER-KEBAB-CASE.
//
//	LogLevel → LOG-LEVEL, APIKey → API-KEY
func UpperKebab(s string) string { return strings.ToUpper(strings.Join(splitWords(s), "-")) }

// LowerSnake converts a CamelCase Go field name to lower_snake_case.
//
//	LogLevel → log_level, APIKey → api_key
func LowerSnake(s string) string { return strings.Join(splitWords(s), "_") }

// UpperSnake converts a CamelCase Go field name to UPPER_SNAKE_CASE.
//
//	LogLevel → LOG_LEVEL, APIKey → API_KEY
func UpperSnake(s string) string { return strings.ToUpper(strings.Join(splitWords(s), "_")) }

// PascalCase converts a CamelCase Go field name to PascalCase (all words title-cased, joined).
// Because this splits on CamelCase word boundaries first, it normalizes acronyms:
//
//	LogLevel → LogLevel, APIKey → ApiKey, DBConfig → DbConfig
func PascalCase(s string) string {
	words := splitWords(s)
	for i, w := range words {
		words[i] = titleWord(w)
	}
	return strings.Join(words, "")
}

// CamelCase converts a CamelCase Go field name to camelCase (first word lowercase, rest title-cased).
// Normalizes acronyms at the word boundary:
//
//	LogLevel → logLevel, APIKey → apiKey, DBConfig → dbConfig
func CamelCase(s string) string {
	words := splitWords(s)
	if len(words) == 0 {
		return s
	}
	for i := 1; i < len(words); i++ {
		words[i] = titleWord(words[i])
	}
	return strings.Join(words, "")
}

// AsIs returns the field name unchanged.
//
//	LogLevel → LogLevel
func AsIs(s string) string { return s }

// splitWords splits a CamelCase identifier into lowercase words.
// A word boundary is inserted before an uppercase rune when:
//   - the previous rune is lowercase or a digit, OR
//   - the previous rune is uppercase AND the next rune is lowercase
//     (handles acronyms: "APIKey" → ["api", "key"])
func splitWords(s string) []string {
	runes := []rune(s)
	n := len(runes)
	var words []string
	var cur strings.Builder
	for i, r := range runes {
		if !unicode.IsUpper(r) {
			cur.WriteRune(r)
			continue
		}
		if i > 0 && camelBoundary(runes, i, n) && cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
		cur.WriteRune(unicode.ToLower(r))
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}

// camelBoundary reports whether position i in runes is a CamelCase word boundary.
func camelBoundary(runes []rune, i, n int) bool {
	prev := runes[i-1]
	return unicode.IsLower(prev) || unicode.IsDigit(prev) ||
		(unicode.IsUpper(prev) && i+1 < n && unicode.IsLower(runes[i+1]))
}

// titleWord uppercases the first rune of a word.
func titleWord(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
