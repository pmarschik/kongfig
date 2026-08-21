// Package editsplice puts a set of text replacements into a document in one
// pass, so an in-place editor can collect the edits a data change implies and
// splice them all at the end.
//
// Collecting first is what makes a refusal safe: an editor that writes as it
// walks leaves half an edit behind when it reaches a change the format cannot
// express. Splicing in one pass is what keeps the cost of that off the size of
// the document — a splice per edit copies the whole document per edit.
package editsplice

import (
	"errors"
	"fmt"
	"slices"
)

// Edit replaces the bytes of a document in [Start, End) with Text. An empty
// span inserts at Start, and empty text deletes the span.
type Edit struct {
	Text  string
	Start int
	End   int
}

var (
	// ErrOverlap reports two edits over the same bytes. Each would rewrite what
	// the other read, so no order gives both of them what they asked for.
	ErrOverlap = errors.New("two edits cover the same text")

	// ErrRange reports an edit that names bytes the document does not have, or
	// a span that ends before it starts.
	ErrRange = errors.New("an edit falls outside the document")
)

// Ordered returns the edits sorted by where they start, keeping the order they
// arrived in among the edits that start at the same offset. That order is what
// an editor relies on when it writes a new item in front of an old one that goes
// away: both land on the same offset, and the one collected first goes in first.
//
// It errors for two edits over the same bytes ([ErrOverlap]) and for a span no
// document could have — one that starts below zero or ends before it starts
// ([ErrRange]). Whether an edit fits a given document is a question about that
// document, so [Apply] asks it.
func Ordered(edits []Edit) ([]Edit, error) {
	ordered := slices.Clone(edits)
	slices.SortStableFunc(ordered, func(a, b Edit) int { return a.Start - b.Start })
	kept := 0 // the offset up to which the edits before this one have claimed
	for _, ed := range ordered {
		if ed.Start < 0 {
			return nil, fmt.Errorf("%w: [%d,%d) starts before the document", ErrRange, ed.Start, ed.End)
		}
		if ed.End < ed.Start {
			return nil, fmt.Errorf("%w: [%d,%d) ends before it starts", ErrRange, ed.Start, ed.End)
		}
		if ed.Start < kept {
			return nil, fmt.Errorf("%w: [%d,%d) starts inside the edit before it", ErrOverlap, ed.Start, ed.End)
		}
		kept = ed.End
	}
	return ordered, nil
}

// Apply returns src with the edits spliced in. The edits may arrive in any
// order; edits that start at the same offset go in in the order they are given.
//
// It errors rather than writing a document that no order of the edits describes:
// [ErrOverlap] for two edits over the same bytes, [ErrRange] for an edit outside
// src. The caller reports it — only the caller knows what to call the document it
// could not edit.
func Apply(src []byte, edits []Edit) ([]byte, error) {
	if len(edits) == 0 {
		return slices.Clone(src), nil
	}
	ordered, err := Ordered(edits)
	if err != nil {
		return nil, err
	}
	size := len(src)
	for _, ed := range ordered {
		if ed.End > len(src) {
			return nil, fmt.Errorf("%w: [%d,%d) of %d bytes", ErrRange, ed.Start, ed.End, len(src))
		}
		size += len(ed.Text) - (ed.End - ed.Start)
	}
	out := make([]byte, 0, max(size, 0))
	kept := 0 // the offset in src up to which out is written
	for _, ed := range ordered {
		out = append(out, src[kept:ed.Start]...)
		out = append(out, ed.Text...)
		kept = ed.End
	}
	return append(out, src[kept:]...), nil
}
