package json

import (
	"slices"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/internal/editalign"
)

// This file edits the arrays of a document. An array is the one place where an
// editor has to work out which written element is which wanted element before it
// can touch anything: the elements that did not change keep their text, and with
// it the comments beside them, while the change lands on the element it is about.
// See [editalign].

// list edits an array element by element: the element that changed is rewritten
// where it is written, an element that went away takes its text with it, and a
// new one joins the array where the data puts it.
//
// New elements go in before the old ones come out, for the reason
// [docEditor.object] gives: both can land on the comma after the last element
// that stays.
func (e *docEditor) list(node *docNode, got any, want []any, path string) error {
	if node == nil || node.kind != nodeArray {
		return e.rewrite(node, want, path)
	}
	gotList, _ := wantList(got)
	ops := elemOps(node.elems, gotList, want)
	drop := dropped(ops, len(node.elems))
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		switch op.Kind {
		case editalign.Rewrite:
			if err := e.value(node.elems[op.Doc], elemAt(gotList, op.Doc), want[op.Want], joinIndex(path, op.Want)); err != nil {
				return err
			}
		case editalign.Delete:
			continue // taken out below, once the new elements know where to go
		case editalign.Insert:
			run := insertRun(ops[i:])
			if err := e.insertElems(node, op.Doc, run, want, drop, path); err != nil {
				return err
			}
			i += len(run) - 1
		}
	}
	return e.removeElems(node, drop, path)
}

// elemOps matches the elements the document writes against the elements the
// caller wants. It pairs by position when the parsed array and the written array
// disagree about how many elements there are — there is no identity to match on
// then, and guessing one would move text further than the data moved.
func elemOps(elems []*docNode, got, want []any) []editalign.Op {
	if len(got) != len(elems) {
		return editalign.Positional(len(elems), len(want))
	}
	return editalign.Elements(got, want, kongfig.EqualValues)
}

// elemAt reads a parsed element, for an array the editor is walking by the
// document's elements rather than by the data's.
func elemAt(list []any, i int) any {
	if i < 0 || i >= len(list) {
		return nil
	}
	return list[i]
}

// dropped reads the whole script before anything is written, because where a new
// element goes depends on which of the written ones are still going to be there.
func dropped(ops []editalign.Op, elems int) []bool {
	drop := make([]bool, elems)
	for _, op := range ops {
		if op.Kind == editalign.Delete {
			drop[op.Doc] = true
		}
	}
	return drop
}

// insertRun is the stretch of inserts at the head of ops that share an anchor.
// They go in as one insertion, so the text of the one that joins them to the
// element before is written once.
func insertRun(ops []editalign.Op) []int {
	wants := []int{ops[0].Want}
	for _, op := range ops[1:] {
		if op.Kind != editalign.Insert || op.Doc != ops[0].Doc {
			break
		}
		wants = append(wants, op.Want)
	}
	return wants
}

// insertElems writes new elements into an array: in front of the element they go
// before, or after the last element that stays when they go at the end.
func (e *docEditor) insertElems(node *docNode, at int, wants []int, want []any, drop []bool, path string) error {
	texts := make([]string, 0, len(wants))
	for _, j := range wants {
		text, err := e.text(want[j], node.indent, node.multiline, joinIndex(path, j))
		if err != nil {
			return err
		}
		texts = append(texts, text)
	}
	if at >= len(node.elems) {
		e.appendItems(node, lastKeptElem(node, drop), texts)
		return nil
	}
	e.insertBefore(node, node.elems[at], texts)
	return nil
}

// removeElems takes out the elements the data no longer has.
func (e *docEditor) removeElems(node *docNode, drop []bool, path string) error {
	for i, out := range drop {
		if !out {
			continue
		}
		if err := e.removeItem(node, node.elems[i].del, i == len(drop)-1, joinIndex(path, i)); err != nil {
			return err
		}
	}
	if len(drop) == 0 || !drop[len(drop)-1] {
		return nil
	}
	e.dropTrailingComma(lastKeptElem(node, drop))
	return nil
}

// lastKeptElem is the last element the document keeps, which is where a new
// element joins the array. There is none when every element goes away.
func lastKeptElem(node *docNode, drop []bool) *docNode {
	for i, d := range slices.Backward(drop) {
		if !d {
			return node.elems[i]
		}
	}
	return nil
}
