// Package mergefuncs provides ready-made [kongfig.MergeFunc] strategies for common
// merge patterns such as slice append, slice replace, and set union.
//
// Usage:
//
//	k.SetMergeFunc("tags", mergefuncs.AppendSlice)
//	k.SetMergeFunc("plugins", mergefuncs.UnionSet)
package mergefuncs

import (
	"fmt"

	kongfig "github.com/pmarschik/kongfig"
)

// AppendSlice is a [kongfig.MergeFunc] that appends incoming slice items to the
// existing slice. If the destination is nil, behaves like ReplaceSlice.
// Falls back to last-writer-wins (returning an error) if src is not a slice.
var AppendSlice kongfig.MergeFunc = func(dst, src any) (any, error) {
	srcSlice, ok := toSlice(src)
	if !ok {
		return nil, fmt.Errorf("mergefuncs.AppendSlice: src is not a slice: %T", src)
	}
	dstSlice, _ := toSlice(dst)
	return append(dstSlice, srcSlice...), nil
}

// ReplaceSlice is a [kongfig.MergeFunc] that replaces the destination slice with
// the incoming slice. Semantically identical to last-writer-wins but documents intent.
// Falls back to last-writer-wins (returning an error) if src is not a slice.
var ReplaceSlice kongfig.MergeFunc = func(_, src any) (any, error) {
	if _, ok := toSlice(src); !ok {
		return nil, fmt.Errorf("mergefuncs.ReplaceSlice: src is not a slice: %T", src)
	}
	return src, nil
}

// UnionSet is a [kongfig.MergeFunc] that merges two slices, deduplicating by string
// representation. Order is preserved: dst items first, then new src items.
// Falls back to last-writer-wins (returning an error) if src is not a slice.
var UnionSet kongfig.MergeFunc = func(dst, src any) (any, error) {
	srcSlice, ok := toSlice(src)
	if !ok {
		return nil, fmt.Errorf("mergefuncs.UnionSet: src is not a slice: %T", src)
	}
	dstSlice, _ := toSlice(dst)

	seen := make(map[string]bool, len(dstSlice))
	for _, v := range dstSlice {
		seen[fmt.Sprintf("%v", v)] = true
	}
	result := make([]any, len(dstSlice), len(dstSlice)+len(srcSlice))
	copy(result, dstSlice)
	for _, v := range srcSlice {
		if key := fmt.Sprintf("%v", v); !seen[key] {
			result = append(result, v)
			seen[key] = true
		}
	}
	return result, nil
}

func toSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, true
	}
	s, ok := v.([]any)
	return s, ok
}
