package kongfig

import (
	"strings"

	"github.com/pmarschik/kongfig/schema"
)

// MapSplitSpec is an alias for [schema.MapSplitSpec].
// Prefer this import path in application code; importing the schema sub-package
// directly is not necessary for typical usage.
type MapSplitSpec = schema.MapSplitSpec

// WithSplits registers per-path split codecs and render-time separators.
// For each path in splits, string values loaded from providers are split by the
// registered separator into []string before merging. The env renderer uses the
// separators to rejoin []string values for output.
//
// Prefer [NewFor] which calls [schema.SplitPaths] automatically from kongfig sep struct tag options.
func WithSplits(splits map[string]string) Option {
	return func(k *Kongfig) {
		WithPathMeta(SplitSepKey, splits)(k)
		paths := make(map[string]anyCodec, len(splits))
		for path, sep := range splits {
			paths[path] = anyCodec{
				decode: func(v any) (any, error) {
					s, ok := v.(string)
					if !ok {
						return v, nil // already a slice (e.g. from YAML/TOML), pass through
					}
					return splitEscaped(s, sep), nil
				},
			}
		}
		mergeCodecPaths(k, paths)
	}
}

// WithMapSplits registers per-path map-split codecs and render-time specs.
// For each path in splits, string values loaded from providers are parsed as
// key=value pairs into map[string]string before merging. The env renderer uses
// the specs to rejoin map[string]string values for output.
//
// Prefer [NewFor] which calls [schema.MapSplitPaths] automatically from kongfig sep/kvsep struct tag options.
func WithMapSplits(splits map[string]MapSplitSpec) Option {
	return func(k *Kongfig) {
		WithPathMeta(MapSplitSpecKey, splits)(k)
		paths := make(map[string]anyCodec, len(splits))
		for path, spec := range splits {
			paths[path] = anyCodec{
				decode: func(v any) (any, error) {
					s, ok := v.(string)
					if !ok {
						return v, nil // already a map (e.g. from YAML/TOML), pass through
					}
					result := make(map[string]string)
					for _, pair := range splitEscaped(s, spec.Sep) {
						k, val, found := cutEscaped(pair, spec.KVSep)
						if k == "" || !found {
							continue
						}
						result[k] = val
					}
					return result, nil
				},
			}
		}
		mergeCodecPaths(k, paths)
	}
}

// splitEscaped splits s on sep, treating \sep as a literal sep (escape sequence).
// The backslash is consumed and the sep character(s) appear verbatim in the token.
// Empty sep returns []string{s}.
func splitEscaped(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	var result []string
	var cur strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\\' && strings.HasPrefix(s[i+1:], sep) {
			cur.WriteString(sep)
			i += 1 + len(sep)
			continue
		}
		if strings.HasPrefix(s[i:], sep) {
			result = append(result, cur.String())
			cur.Reset()
			i += len(sep)
			continue
		}
		cur.WriteByte(s[i])
		i++
	}
	return append(result, cur.String())
}

// cutEscaped finds the first unescaped occurrence of sep in s, returning the
// text before and after it. \sep in s is treated as a literal sep (escape sequence).
// Returns (s, "", false) if sep is not found.
func cutEscaped(s, sep string) (before, after string, found bool) {
	if sep == "" {
		return s, "", false
	}
	var buf strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\\' && strings.HasPrefix(s[i+1:], sep) {
			buf.WriteString(sep)
			i += 1 + len(sep)
			continue
		}
		if strings.HasPrefix(s[i:], sep) {
			return buf.String(), s[i+len(sep):], true
		}
		buf.WriteByte(s[i])
		i++
	}
	return buf.String(), "", false
}
