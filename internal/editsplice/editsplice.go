// Package editsplice puts a set of text replacements into a document in one
// pass, so an in-place editor can collect the edits a data change implies and
// splice them all at the end.
//
// Collecting first is what makes a refusal safe: an editor that writes as it
// walks leaves half an edit behind when it reaches a change the format cannot
// express. Splicing in one pass is what keeps the cost of that off the size of
// the document — a splice per edit copies the whole document per edit.
package editsplice

import "slices"

// Edit replaces the bytes of a document in [Start, End) with Text. An empty
// span inserts at Start, and empty text deletes the span.
type Edit struct {
	Text  string
	Start int
	End   int
}

// Apply returns src with the edits spliced in. The edits may arrive in any
// order; edits that start at the same offset go in in the order they are given.
//
// ok is false when two edits cover the same bytes. Each would rewrite what the
// other read, so no order gives both of them what they asked for, and a refusal
// is the only safe answer. The caller reports it: only the caller knows what to
// call the document it could not edit.
func Apply(src []byte, edits []Edit) ([]byte, bool) {
	if len(edits) == 0 {
		return slices.Clone(src), true
	}
	ordered := slices.Clone(edits)
	slices.SortStableFunc(ordered, func(a, b Edit) int { return a.Start - b.Start })
	size := len(src)
	for _, ed := range ordered {
		size += len(ed.Text) - (ed.End - ed.Start)
	}
	out := make([]byte, 0, max(size, 0))
	kept := 0 // the offset in src up to which out is written
	for _, ed := range ordered {
		if ed.Start < kept {
			return nil, false
		}
		out = append(out, src[kept:ed.Start]...)
		out = append(out, ed.Text...)
		kept = ed.End
	}
	return append(out, src[kept:]...), true
}
