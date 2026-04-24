// Package schema provides struct-tag parsing, key validation, and reflection
// utilities for deriving kongfig configuration metadata from Go struct types.
// It performs struct-tag reflection for kongfig — it does NOT generate or validate
// JSON Schema (or any schema language).
//
// The key types and functions in this package are:
//   - [ParseFieldTag] — parses a kongfig struct tag into a [FieldTag]
//   - [ValidateKeyName] — validates a single key segment
//   - [RedactedPaths], [SplitPaths], [MapSplitPaths], [ConfigPaths] — reflect over T
//   - [DefaultNameMapper] — the fallback name mapper used by [ParseFieldTag]
package schema

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/pmarschik/kongfig/casing"
)

// DefaultNameMapper is the [casing.NameMapper] applied by [ParseFieldTag] when
// the kongfig struct tag has no explicit name segment. Defaults to [casing.KebabCase].
//
// Mutability: this is a mutable package-level variable. It is safe to replace at
// program startup (before any calls to [ConfigPaths], [RedactedPaths], [SplitPaths],
// [MapSplitPaths], or [ParseFieldTag]). It is NOT safe to replace concurrently with
// any function that reads it — no synchronization is provided.
var DefaultNameMapper casing.NameMapper = casing.KebabCase

// FieldTag is the parsed result of a kongfig struct tag value.
// Use [ParseFieldTag] to construct it.
//
// Subsystems that need to interpret kongfig struct tags (e.g. validation annotations,
// split transforms) should call ParseFieldTag and read [FieldTag.Extras] rather than
// maintaining their own tag namespaces. The set of built-in options handled here is
// the authoritative source; unknown options land in Extras unchanged.
type FieldTag struct {
	Redacted           *bool
	ConfigPathPriority *int    // nil = no explicit priority; non-nil = priority value
	Default            *string // nil = no default annotation; non-nil = value from default= tag option
	Name               string
	Codec              string // from codec=name tag option; empty if not set
	Extras             []string
	Skip               bool
	Squash             bool
	IsConfigPath       bool
}

// ParseFieldTag parses a kongfig struct tag value.
// fieldName is the Go struct field name, used as the fallback key name when tag
// is empty or has an empty name segment; the fallback is produced by [DefaultNameMapper].
//
// Recognized structural options are decoded into typed [FieldTag] fields and do
// not appear in [FieldTag.Extras]:
//
//	squash, redacted, redacted=false, config-path, config-path=N
//
// All other options land in Extras verbatim (including quoted values like sep=',').
// Use [ParseExtraValue] to extract a key=value extra with optional single-quote unquoting.
//
// Option values may be single-quoted to allow commas and equals signs:
//
//	kongfig:"tags,sep=','"                 // sep extra, value ","
//	kongfig:"labels,sep=',',kvsep='='"    // sep+kvsep extras for maps
func ParseFieldTag(tag, fieldName string) FieldTag {
	fallback := DefaultNameMapper(fieldName)
	if tag == "-" {
		return FieldTag{Name: fallback, Skip: true}
	}

	name, rest, _ := strings.Cut(tag, ",")
	if name == "" {
		name = fallback
	}
	for seg := range strings.SplitSeq(name, ".") {
		if err := ValidateKeyName(seg); err != nil {
			panic(fmt.Sprintf("kongfig: struct tag on field %q: %v", fieldName, err))
		}
	}

	ft := FieldTag{Name: name}
	for _, opt := range splitTagOpts(rest) {
		switch opt {
		case "":
			// empty segment between commas — skip
		case "squash":
			ft.Squash = true
		case "redacted":
			b := true
			ft.Redacted = &b
		case "redacted=false":
			b := false
			ft.Redacted = &b
		case "config-path":
			ft.IsConfigPath = true
		default:
			if !applyPrefixedTagOpt(opt, &ft) {
				ft.Extras = append(ft.Extras, opt)
			}
		}
	}
	return ft
}

// applyPrefixedTagOpt handles tag options of the form "key=value".
// Returns true if the option was recognized and applied to ft.
func applyPrefixedTagOpt(opt string, ft *FieldTag) bool {
	if val, ok := strings.CutPrefix(opt, "config-path="); ok {
		ft.IsConfigPath = true
		if n, err := strconv.Atoi(val); err == nil {
			ft.ConfigPathPriority = &n
		}
		return true
	}
	if val, ok := strings.CutPrefix(opt, "default="); ok {
		s := unquoteSingleQuotes(val)
		ft.Default = &s
		return true
	}
	if val, ok := strings.CutPrefix(opt, "codec="); ok {
		ft.Codec = unquoteSingleQuotes(val)
		return true
	}
	return false
}

// ParseExtraValue searches extras for an entry of the form key='value' or key=value
// and returns the unquoted value. Single-quoted values allow commas and equals signs.
// Returns ("", false) if no entry with that key is found.
//
// This is the extension mechanism for subsystems that add their own annotations
// to the kongfig struct tag:
//
//	type Config struct {
//	    Tags []string `kongfig:"tags,sep=','"`    // sep annotation, value ","
//	}
//
//	sep, ok := schema.ParseExtraValue(ft.Extras, "sep")
func ParseExtraValue(extras []string, key string) (value string, found bool) {
	prefix := key + "="
	for _, e := range extras {
		if val, ok := strings.CutPrefix(e, prefix); ok {
			return unquoteSingleQuotes(val), true
		}
	}
	return "", false
}

// ValidateKeyName reports whether name is a legal single config key segment.
// The characters '[' and ']' are reserved for bracket-index path notation and
// must not appear in key names. '.' is the path separator and is also disallowed
// within a single segment (dots in a kongfig tag value are path separators between
// segments, not part of any one segment).
// Returns nil if name is valid.
//
// Called automatically by [ParseFieldTag] for each dot-separated segment;
// providers generating dynamic key names should call this to detect conflicts early.
func ValidateKeyName(name string) error {
	for _, c := range name {
		switch c {
		case '[', ']':
			return fmt.Errorf("key name %q contains reserved character %q (reserved for bracket-index notation)", name, string(c))
		case '.':
			return fmt.Errorf("key name %q contains '.': use dots only as path separators between segments, not inside a segment name", name)
		}
	}
	return nil
}

// MapSplitSpec describes how to parse and render a map[string]T from a single env var string.
// Sep separates key-value pairs from each other (e.g. "," for "k1=v1,k2=v2").
// KVSep separates each key from its value within a pair (e.g. "=" for "k1=v1").
//
// Both are set via the kongfig struct tag options sep and kvsep:
//
//	Labels map[string]string `kongfig:"labels,sep=',',kvsep='='"`
type MapSplitSpec struct {
	Sep   string
	KVSep string
}

// ConfigPathEntry describes a struct field tagged with the config-path option in
// its kongfig tag, which holds the path to an additional config file to load.
// Use [ConfigPaths] to collect and sort these entries from a config struct type.
type ConfigPathEntry struct {
	Key         string // dot-path in config (e.g., "system-config-path")
	Priority    int    // only meaningful when HasPriority is true
	HasPriority bool   // true if an explicit numeric priority was specified
}

// RedactedPaths reflects on T and returns the set of dot-paths whose kongfig tags
// include "redacted". Redaction is inherited by nested struct fields and can be
// overridden per-field with "redacted=false".
//
// Example:
//
//	type Config struct {
//	    DB DBConfig `kongfig:"db,redacted"`
//	}
//	type DBConfig struct {
//	    Host     string `kongfig:"host,redacted=false"` // not redacted
//	    Password string `kongfig:"password"`             // redacted (inherited)
//	}
func RedactedPaths[T any]() map[string]bool {
	out := make(map[string]bool)
	collectRedacted(reflect.TypeFor[T](), "", false, out)
	return out
}

func collectRedacted(typ reflect.Type, prefix string, parentRedacted bool, out map[string]bool) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for field := range typ.Fields() {
		if field.Anonymous {
			collectRedacted(field.Type, prefix, parentRedacted, out)
			continue
		}
		if !field.IsExported() {
			continue
		}

		ft := ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if ft.Skip {
			continue
		}

		path := ft.Name
		if prefix != "" {
			path = prefix + "." + ft.Name
		}

		redacted := parentRedacted
		if ft.Redacted != nil {
			redacted = *ft.Redacted
		}

		subTyp := field.Type
		for subTyp.Kind() == reflect.Pointer {
			subTyp = subTyp.Elem()
		}
		if subTyp.Kind() == reflect.Struct {
			collectRedacted(field.Type, path, redacted, out)
			continue
		}

		if redacted {
			out[path] = true
		}
	}
}

// SplitPaths reflects on T and returns the set of dot-paths that should be
// split from a delimited string into a slice, keyed by their separator.
// Paths are derived from fields whose kongfig tag includes a sep='<sep>' option
// on slice or array fields.
func SplitPaths[T any]() map[string]string {
	out := make(map[string]string)
	walkStructFields(reflect.TypeFor[T](), "", func(field reflect.StructField, path string, subTyp reflect.Type) {
		ft := ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		sep, ok := ParseExtraValue(ft.Extras, "sep")
		if !ok || sep == "" {
			return
		}
		if subTyp.Kind() != reflect.Slice && subTyp.Kind() != reflect.Array {
			return
		}
		// Only register for slices of primitive element types; slices of structs
		// with kongfig tags are not splittable via a separator.
		elemTyp := subTyp.Elem()
		for elemTyp.Kind() == reflect.Pointer {
			elemTyp = elemTyp.Elem()
		}
		if elemTyp.Kind() != reflect.Struct {
			out[path] = sep
		}
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// MapSplitPaths reflects on T and returns the set of dot-paths that should be
// parsed from a delimited key=value string into a map, keyed by their [MapSplitSpec].
// Paths are derived from map fields whose kongfig tag includes:
//   - sep='<sep>'   — separator between key-value pairs, e.g. "," for "k1=v1,k2=v2"
//   - kvsep='<sep>' — separator between key and value within a pair, e.g. "=" for "k=v"
func MapSplitPaths[T any]() map[string]MapSplitSpec {
	out := make(map[string]MapSplitSpec)
	walkStructFields(reflect.TypeFor[T](), "", func(field reflect.StructField, path string, subTyp reflect.Type) {
		ft := ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		sep, hasSep := ParseExtraValue(ft.Extras, "sep")
		kvSep, hasKVSep := ParseExtraValue(ft.Extras, "kvsep")
		if !hasSep && !hasKVSep {
			return
		}
		if subTyp.Kind() != reflect.Map {
			return
		}
		// Only register for maps with primitive (non-struct) value types.
		valTyp := subTyp.Elem()
		for valTyp.Kind() == reflect.Pointer {
			valTyp = valTyp.Elem()
		}
		if valTyp.Kind() != reflect.Struct {
			spec := MapSplitSpec{Sep: ",", KVSep: "="}
			if hasSep {
				spec.Sep = sep
			}
			if hasKVSep {
				spec.KVSep = kvSep
			}
			out[path] = spec
		}
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// ConfigPaths reflects on T and returns the set of dot-paths whose kongfig tag
// includes the config-path option, sorted so that:
//   - Entries with an explicit numeric priority come first, ascending (0 = highest).
//   - Entries with no priority follow, in struct discovery order.
//
// Only string fields are considered; non-string fields are silently skipped.
func ConfigPaths[T any]() []ConfigPathEntry {
	var out []ConfigPathEntry
	walkStructFields(reflect.TypeFor[T](), "", func(field reflect.StructField, path string, subTyp reflect.Type) {
		ft := ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if !ft.IsConfigPath {
			return
		}
		if subTyp.Kind() != reflect.String {
			return
		}
		entry := ConfigPathEntry{Key: path}
		if ft.ConfigPathPriority != nil {
			entry.Priority = *ft.ConfigPathPriority
			entry.HasPriority = true
		}
		out = append(out, entry)
	})
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := out[i], out[j]
		if pi.HasPriority != pj.HasPriority {
			return pi.HasPriority // prioritized before unprioritized
		}
		if pi.HasPriority {
			return pi.Priority < pj.Priority // ascending numeric priority
		}
		return false // both unprioritized: stable sort preserves discovery order
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// CodecPathEntry describes a struct field that should be processed by a named or
// type-inferred codec at load time. Use [CodecPaths] to collect these from a config
// struct type.
type CodecPathEntry struct {
	GoType    reflect.Type
	Path      string
	CodecName string
}

// CodecPaths reflects on T and returns all non-primitive fields together with any
// field carrying an explicit codec= annotation. The caller resolves each entry against
// a [codecRegistry] to build a path→codec map used at load time.
//
// A field is included if it has an explicit codec= tag OR its kind is not one of the
// built-in primitive kinds (bool, int*, uint*, float*, complex*, string). Struct fields
// are recursed into; the leaf paths are returned, not the struct path.
func CodecPaths[T any]() []CodecPathEntry {
	var out []CodecPathEntry
	walkStructFields(reflect.TypeFor[T](), "", func(field reflect.StructField, path string, subTyp reflect.Type) {
		ft := ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if ft.Codec != "" {
			out = append(out, CodecPathEntry{Path: path, CodecName: ft.Codec, GoType: subTyp})
			return
		}
		if !isPrimitiveKind(subTyp.Kind()) {
			out = append(out, CodecPathEntry{Path: path, GoType: subTyp})
		}
	})
	return out
}

// isPrimitiveKind reports whether k is one of the scalar kinds that do not need
// a codec for round-trip encoding/decoding.
func isPrimitiveKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128,
		reflect.String:
		return true
	default:
		return false
	}
}

// walkStructFields walks the exported, non-skipped fields of typ (following embedded
// structs), calling fn for each non-struct leaf field with its resolved dot-path and
// dereferenced type. Struct-typed fields are recursed into automatically.
func walkStructFields(typ reflect.Type, prefix string, fn func(field reflect.StructField, path string, subTyp reflect.Type)) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for field := range typ.Fields() {
		if field.Anonymous {
			walkStructFields(field.Type, prefix, fn)
			continue
		}
		if !field.IsExported() {
			continue
		}
		ft := ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if ft.Skip {
			continue
		}
		path := ft.Name
		if prefix != "" {
			path = prefix + "." + ft.Name
		}
		subTyp := field.Type
		for subTyp.Kind() == reflect.Pointer {
			subTyp = subTyp.Elem()
		}
		if subTyp.Kind() == reflect.Struct {
			walkStructFields(field.Type, path, fn)
			continue
		}
		fn(field, path, subTyp)
	}
}

// splitTagOpts splits an option string on commas, skipping commas inside
// single-quoted values. Trims whitespace from each segment.
func splitTagOpts(s string) []string {
	if s == "" {
		return nil
	}
	var opts []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '\'':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			opts = append(opts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	opts = append(opts, strings.TrimSpace(cur.String()))
	return opts
}

// unquoteSingleQuotes strips a matching outer pair of single quotes.
func unquoteSingleQuotes(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}
