package toml

import (
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/internal/editalign"
)

// This file edits the lists of a document — an array, and a run of [[sections]],
// which is a list too. A list is the one place where an editor has to work out
// which written element is which wanted element before it can touch anything: the
// elements that did not change keep their text, and with it the comments beside
// them, while the change lands on the element it is about. See [editalign].

// elemOps matches the elements the document writes against the elements the
// caller wants. It pairs by position when the parsed list and the written list
// disagree about how many elements there are — there is no identity to match on
// then, and guessing one would move text further than the data moved.
func elemOps(elems []*docNode, got, want []any) []editalign.Op {
	if len(got) != len(elems) {
		return editalign.Positional(len(elems), len(want))
	}
	return editalign.Elements(got, want, kongfig.EqualValues)
}

// elemAt reads a parsed element, for a list the editor is walking by the
// document's elements rather than by the data's.
func elemAt(list []any, i int) any {
	if i < 0 || i >= len(list) {
		return nil
	}
	return list[i]
}

// insertRun is the stretch of inserts at the head of ops that share an anchor.
// They go in as one insertion, so they keep their order: two insertions at the
// same offset would come out in the order the splice reaches them, which is the
// reverse of this one.
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

// list edits a list element by element: the element that changed is rewritten
// where it is written, an element that went away takes its text with it, and a
// new one joins the list where the data puts it.
func (e *docEditor) list(node *docNode, got any, want []any, path string) error {
	if node == nil || (node.kind != nodeArray && node.kind != nodeTableArray) {
		return e.rewrite(node, want, path)
	}
	gotList, _ := wantList(got)
	ops := elemOps(node.elems, gotList, want)
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		switch op.Kind {
		case editalign.Rewrite:
			if err := e.value(node.elems[op.Doc], elemAt(gotList, op.Doc), want[op.Want], joinIndex(path, op.Want)); err != nil {
				return err
			}
		case editalign.Delete:
			if err := e.removeElem(node, node.elems[op.Doc], joinIndex(path, op.Doc)); err != nil {
				return err
			}
		case editalign.Insert:
			run := insertRun(ops[i:])
			if err := e.insertElems(node, op.Doc, run, want, path); err != nil {
				return err
			}
			i += len(run) - 1
		}
	}
	return nil
}

// insertElems writes new elements into a list, in the layout the list already
// uses: before the element they go in front of, or after the last one when they
// go at the end.
func (e *docEditor) insertElems(node *docNode, at int, wants []int, want []any, path string) error {
	if node.kind != nodeArray {
		return cannotEdit(path, "a list of sections takes no new element")
	}
	if at >= len(node.elems) {
		values := make([]any, 0, len(wants))
		for _, j := range wants {
			values = append(values, want[j])
		}
		return e.appendElems(node, values)
	}
	var text strings.Builder
	for _, j := range wants {
		if node.multiline {
			text.WriteString(node.indent + tomlValue(want[j]) + ",\n")
			continue
		}
		text.WriteString(tomlValue(want[j]) + ", ")
	}
	elem := node.elems[at]
	if node.multiline {
		e.insert(e.elemStart(elem), text.String())
		return nil
	}
	e.insert(elem.value.start, text.String())
	return nil
}

// appendElems adds elements after the last one, in the layout the list already
// uses: one per line for a list written across lines, on the line for one
// written on a single line. All of them go in as one insertion, so they keep
// their order.
func (e *docEditor) appendElems(node *docNode, values []any) error {
	var text strings.Builder
	for i, v := range values {
		switch {
		case node.multiline:
			if i == 0 && !node.comma && len(node.elems) > 0 {
				text.WriteString(",")
			}
			text.WriteString("\n" + node.indent + tomlValue(v) + ",")
		case len(node.elems) == 0 && i == 0:
			text.WriteString(tomlValue(v))
		default:
			text.WriteString(", " + tomlValue(v))
		}
	}
	e.insert(node.insertAt, text.String())
	return nil
}

// removeElem takes an element out of a list: its whole line in a list written
// across lines, otherwise the element and the separator that follows it.
func (e *docEditor) removeElem(node, elem *docNode, path string) error {
	if elem.del.empty() {
		return cannotEdit(path, "there is no text to remove")
	}
	if node.kind == nodeTableArray {
		e.remove(elem.del)
		return nil
	}
	if node.multiline {
		at := e.wholeLines(elem.del)
		at.start = e.elemStart(elem)
		e.remove(at)
		return nil
	}
	e.remove(e.withSeparator(elem.del))
	return nil
}

// elemStart is where the text of an element on its own line begins: that line,
// or the first of the comment lines above it. A comment written directly above
// an element is about that element, so it belongs to the element's text — it goes
// away with the element, and a new element goes in above it rather than between
// the two.
func (e *docEditor) elemStart(elem *docNode) int {
	start := lineStartOf(e.src, elem.del.start)
	for start > 0 {
		above := lineStartOf(e.src, start-1)
		if !e.commentLine(above) {
			return start
		}
		start = above
	}
	return start
}

// commentLine reports whether the line at lineStart holds nothing but a comment.
func (e *docEditor) commentLine(lineStart int) bool {
	i := lineStart
	for i < len(e.src) && isInlineSpace(e.src[i]) {
		i++
	}
	return i < len(e.src) && e.src[i] == '#'
}
