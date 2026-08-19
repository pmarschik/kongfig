package render

import (
	"context"
	"reflect"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
)

// OmitEmpty reports whether the value at path should be left out of the output
// because the path is marked omitempty ([kongfig.OmitEmptyKey], populated by
// [kongfig.NewFor] from omitempty struct tags) and the value holds nothing:
// nil, an empty string, a zero number, false, or an empty list, map or table.
//
// A redacted value is never empty — its placeholder stands in for a value that
// is set, and dropping the key would say the opposite. Renderers that cannot
// express absence (a fixed-key format, say) may ignore the mark.
func OmitEmpty(ctx context.Context, path string, v any) bool {
	marks := kongfig.OmitEmptyKey.GetAll(ctx)
	if len(marks) == 0 {
		return false
	}
	if !matchesOmitEmpty(marks, path) {
		return false
	}
	if rv, ok := v.(kongfig.RenderedValue); ok {
		if rv.Redacted {
			return false
		}
		v = rv.Value
	}
	return isEmptyValue(v)
}

// matchesOmitEmpty reports whether path is marked, either exactly or by a
// pattern whose "*" segments each match one segment of path.
func matchesOmitEmpty(marks map[string]bool, path string) bool {
	if marks[path] {
		return true
	}
	segs := strings.Split(path, ".")
	for pattern, on := range marks {
		if !on || !strings.Contains(pattern, "*") {
			continue
		}
		if matchPathPattern(strings.Split(pattern, "."), segs) {
			return true
		}
	}
	return false
}

// matchPathPattern reports whether a path split into segs is matched by a
// pattern split the same way, each "*" segment standing for one segment of path.
// Shared by the path marks that are written on a type inside a map.
func matchPathPattern(pattern, segs []string) bool {
	if len(pattern) != len(segs) {
		return false
	}
	for i, p := range pattern {
		if p != "*" && p != segs[i] {
			return false
		}
	}
	return true
}

// isEmptyValue mirrors the emptiness the encoding packages use for omitempty,
// through reflection so typed slices and maps are covered too.
func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Array, reflect.Slice, reflect.Map:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}
