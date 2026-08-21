package yaml

import (
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/internal/editalign"
	goyaml "gopkg.in/yaml.v3"
)

// This file edits the lists of a document. A list is the one place where an
// editor has to work out which written element is which wanted element before it
// can touch anything: the elements that did not change keep their text, and with
// it the comments beside them, while the change lands on the element it is about.
// See [editalign].

// elemOps matches the elements the document writes against the elements the
// caller wants. It pairs by position when the parsed list and the written list
// disagree about how many elements there are — there is no identity to match on
// then, and guessing one would move text further than the data moved.
func elemOps(nodes []*goyaml.Node, got, want []any) []editalign.Op {
	if len(got) != len(nodes) {
		return editalign.Positional(len(nodes), len(want))
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

// sequence edits a list element by element: the element that changed is
// rewritten where it is written, an element that went away takes its lines with
// it, and a new one joins the list where the data puts it.
func (e *docEditor) sequence(node *goyaml.Node, got any, want []any, path string, indent int) error {
	if node.Style&goyaml.FlowStyle != 0 {
		return e.flowSequence(node, got, want, path)
	}
	gotList, _ := wantList(got)
	ops := elemOps(node.Content, gotList, want)
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		switch op.Kind {
		case editalign.Rewrite:
			elem := node.Content[op.Doc]
			dash, ok := e.dashIndent(elem)
			if !ok {
				dash = indent
			}
			if err := e.value(elem, elemAt(gotList, op.Doc), want[op.Want], joinIndex(path, op.Want), dash); err != nil {
				return err
			}
		case editalign.Delete:
			if err := e.removeElem(node.Content[op.Doc], joinIndex(path, op.Doc)); err != nil {
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

// removeElem takes out the lines that write one element: the element's own, the
// lines indented under it, and the comment lines directly above it, which the
// author wrote about the element that is going away.
func (e *docEditor) removeElem(elem *goyaml.Node, path string) error {
	dash, ok := e.dashIndent(elem)
	if !ok {
		return cannotEdit(path, "the element does not start its own line")
	}
	e.remove(span{start: e.elemStart(elem), end: e.blockEnd(e.offset(elem), dash)})
	return nil
}

// insertElems writes new elements into a block list: before the element they go
// in front of, or after the last one when they go at the end. They are indented
// and quoted like the element they join, so the list still reads as one list.
func (e *docEditor) insertElems(node *goyaml.Node, at int, wants []int, want []any, path string) error {
	if len(node.Content) == 0 {
		return cannotEdit(path, "the list has no element to write after")
	}
	anchor := node.Content[min(at, len(node.Content)-1)]
	dash, ok := e.dashIndent(anchor)
	if !ok {
		return cannotEdit(path, "the element it would join does not start its own line")
	}
	var text strings.Builder
	for _, j := range wants {
		written, err := encode(want[j], anchor, joinIndex(path, j))
		if err != nil {
			return err
		}
		if strings.Contains(written, "\n") {
			return cannotEdit(joinIndex(path, j), "the element does not fit on one line")
		}
		text.WriteString(strings.Repeat(" ", dash) + "- " + written + "\n")
	}
	if at < len(node.Content) {
		e.insert(e.elemStart(anchor), text.String())
		return nil
	}
	e.insert(e.blockEnd(e.offset(anchor), dash), text.String())
	return nil
}

// elemStart is where the text of an element begins: its own line, or the first
// of the comment lines above it. A comment written directly above an element is
// about that element, so it belongs to the element's text — it goes away with the
// element, and a new element goes in above it rather than between the two.
func (e *docEditor) elemStart(elem *goyaml.Node) int {
	start := e.lineStartAt(e.offset(elem))
	for start > 0 {
		above := e.lineStartAt(start - 1)
		if !e.commentLine(above) {
			return start
		}
		start = above
	}
	return start
}

// commentLine reports whether the line at lineStart holds nothing but a comment.
func (e *docEditor) commentLine(lineStart int) bool {
	i := lineStart + e.indentAt(lineStart)
	return i < len(e.src) && e.src[i] == '#'
}

// flowSequence edits a [ a, b ] list, which stays on its line.
func (e *docEditor) flowSequence(node *goyaml.Node, got any, want []any, path string) error {
	end, err := e.flowEnd(node, path)
	if err != nil {
		return err
	}
	gotList, _ := wantList(got)
	ops := elemOps(node.Content, gotList, want)
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		switch op.Kind {
		case editalign.Rewrite:
			if err := e.value(node.Content[op.Doc], elemAt(gotList, op.Doc), want[op.Want], joinIndex(path, op.Want), 0); err != nil {
				return err
			}
		case editalign.Delete:
			at, err := e.tokenSpan(node.Content[op.Doc], joinIndex(path, op.Doc))
			if err != nil {
				return err
			}
			e.remove(e.withSeparator(at))
		case editalign.Insert:
			run := insertRun(ops[i:])
			if err := e.insertFlowElems(node, op.Doc, run, want, path, end); err != nil {
				return err
			}
			i += len(run) - 1
		}
	}
	return nil
}

// insertFlowElems writes new elements into a list on one line: before the
// element they go in front of, after the last one, or between the brackets of a
// list that has no element yet.
func (e *docEditor) insertFlowElems(node *goyaml.Node, at int, wants []int, want []any, path string, end int) error {
	parts := make([]string, 0, len(wants))
	for _, j := range wants {
		written, err := encode(want[j], likeScalar(node, at), joinIndex(path, j))
		if err != nil {
			return err
		}
		parts = append(parts, written)
	}
	text := strings.Join(parts, ", ")
	if len(node.Content) == 0 {
		e.insert(end-1, text)
		return nil
	}
	if at < len(node.Content) {
		before, err := e.tokenSpan(node.Content[at], joinIndex(path, at))
		if err != nil {
			return err
		}
		e.insert(before.start, text+", ")
		return nil
	}
	last := len(node.Content) - 1
	after, err := e.tokenSpan(node.Content[last], joinIndex(path, last))
	if err != nil {
		return err
	}
	e.insert(after.end, ", "+text)
	return nil
}

// likeScalar is the element a new one is written like: the one it goes in front
// of, or the last one when it goes at the end. Only a scalar has quoting to
// follow.
func likeScalar(node *goyaml.Node, at int) *goyaml.Node {
	if len(node.Content) == 0 {
		return nil
	}
	like := node.Content[min(at, len(node.Content)-1)]
	if like.Kind != goyaml.ScalarNode {
		return nil
	}
	return like
}
