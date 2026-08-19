package render

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
)

// HasKeyOrder reports whether anything in ctx has a say about the order of keys:
// an explicit [kongfig.WithRenderKeyOrder], the order kongfig derived from the
// documents and the schema, a sortby= mark, or a [kongfig.KeySortFunc].
//
// Marshalers that hand their map straight to an encoder when no order is bound
// ask this first, so a sort hook is not silently dropped along with the order.
func HasKeyOrder(ctx context.Context) bool {
	if order, ok := kongfig.RenderKeyOrderKey.Read(ctx); ok && len(order) > 0 {
		return true
	}
	if order, ok := kongfig.RenderDerivedKeyOrderKey.Read(ctx); ok && len(order) > 0 {
		return true
	}
	if len(kongfig.KeySortByKey.GetAll(ctx)) > 0 {
		return true
	}
	fn, ok := kongfig.RenderKeySortKey.Read(ctx)
	return ok && fn != nil
}

// sortKeys applies the sort hooks to the keys the order rules produced: a
// sortby= mark for path first, then a [kongfig.KeySortFunc], which outranks it.
func sortKeys(ctx context.Context, path string, data kongfig.ConfigData, keys []string) []string {
	if spec := sortBySpec(ctx, path); spec != "" {
		keys = sortByValue(keys, data, spec)
	}
	if fn, ok := kongfig.RenderKeySortKey.Read(ctx); ok && fn != nil {
		keys = reconcileKeys(fn(path, slices.Clone(keys), data), keys)
	}
	return keys
}

// sortBySpec returns the sort spec marked for path, matching a pattern whose
// "*" segments each stand for one segment of path.
func sortBySpec(ctx context.Context, path string) string {
	marks := kongfig.KeySortByKey.GetAll(ctx)
	if len(marks) == 0 {
		return ""
	}
	if spec, ok := marks[path]; ok {
		return spec
	}
	segs := strings.Split(path, ".")
	for pattern, spec := range marks {
		if strings.Contains(pattern, "*") && matchPathPattern(strings.Split(pattern, "."), segs) {
			return spec
		}
	}
	return ""
}

// sortByValue orders keys by the value spec names inside each entry. The sort is
// stable, so entries that compare equal keep the order they arrived in.
func sortByValue(keys []string, data kongfig.ConfigData, spec string) []string {
	field, desc := strings.CutPrefix(spec, "-")
	segs := strings.Split(field, ".")
	out := slices.Clone(keys)
	sort.SliceStable(out, func(i, j int) bool {
		a, aOK := sortValue(data[out[i]], segs)
		b, bOK := sortValue(data[out[j]], segs)
		// Rank first and never reversed: a value that cannot be placed among the
		// others reads last whichever way the rest are sorted.
		ra, rb := sortRank(a, aOK), sortRank(b, bOK)
		if ra != rb {
			return ra < rb
		}
		c := compareSortValues(a, b)
		if desc {
			c = -c
		}
		return c < 0
	})
	return out
}

// sortValue walks segs into the entry v and returns the value it names, looking
// through the [kongfig.RenderedValue] wrappers rendering puts on leaves.
func sortValue(v any, segs []string) (any, bool) {
	for _, seg := range segs {
		v = unwrapRendered(v)
		switch m := v.(type) {
		case kongfig.ConfigData:
			var ok bool
			if v, ok = m[seg]; !ok {
				return nil, false
			}
		case map[string]any:
			var ok bool
			if v, ok = m[seg]; !ok {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return unwrapRendered(v), true
}

func unwrapRendered(v any) any {
	if rv, ok := v.(kongfig.RenderedValue); ok {
		return rv.Value
	}
	return v
}

// Sort ranks, compared ascending in both directions so that what cannot be
// ordered stays out of the way of what can.
const (
	rankNumber = iota
	rankBool
	rankString
	rankUnsortable
)

func sortRank(v any, found bool) int {
	if !found || v == nil {
		return rankUnsortable
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return rankNumber
	case reflect.Bool:
		return rankBool
	case reflect.String:
		return rankString
	default:
		return rankUnsortable
	}
}

// compareSortValues compares two values of the same rank, returning -1, 0 or 1.
func compareSortValues(a, b any) int {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	switch sortRank(a, true) {
	case rankNumber:
		return cmpFloat(numericValue(av), numericValue(bv))
	case rankBool:
		switch {
		case av.Bool() == bv.Bool():
			return 0
		case bv.Bool():
			return -1
		default:
			return 1
		}
	case rankString:
		return strings.Compare(av.String(), bv.String())
	default:
		return 0
	}
}

func numericValue(v reflect.Value) float64 {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(v.Uint())
	default:
		return v.Float()
	}
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// reconcileKeys keeps a comparator honest: the keys it returned are taken in
// that order, minus the ones it invented or repeated, and the keys it left out
// follow in the order they had.
func reconcileKeys(sorted, keys []string) []string {
	known := make(map[string]bool, len(keys))
	for _, k := range keys {
		known[k] = true
	}
	out := make([]string, 0, len(keys))
	taken := make(map[string]bool, len(keys))
	for _, k := range sorted {
		if known[k] && !taken[k] {
			out = append(out, k)
			taken[k] = true
		}
	}
	for _, k := range keys {
		if !taken[k] {
			out = append(out, k)
		}
	}
	return out
}
