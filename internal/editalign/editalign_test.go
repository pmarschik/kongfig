package editalign_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pmarschik/kongfig/internal/editalign"
)

// equal is the comparison an editor hands in; string equality is enough to say
// which elements of a list are the same elements.
func equal(a, b any) bool { return a == b }

// script writes an op list the way a test reads it, so a failure says what the
// aligner decided rather than which struct fields differ.
func script(ops []editalign.Op) string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		switch op.Kind {
		case editalign.Rewrite:
			out = append(out, fmt.Sprintf("rewrite %d as %d", op.Doc, op.Want))
		case editalign.Delete:
			out = append(out, fmt.Sprintf("delete %d", op.Doc))
		case editalign.Insert:
			out = append(out, fmt.Sprintf("insert %d before %d", op.Want, op.Doc))
		}
	}
	return strings.Join(out, "; ")
}

func list(vs ...string) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

func TestElements(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
		doc  []any
		to   []any
	}{{
		name: "nothing changed is nothing to do",
		doc:  list("a", "b", "c"),
		to:   list("a", "b", "c"),
		want: "",
	}, {
		// The element that changed is the element that gets rewritten: its text,
		// and the comment beside it, stay where the author put them.
		name: "one element changed",
		doc:  list("a", "b", "c"),
		to:   list("a", "x", "c"),
		want: "rewrite 1 as 1",
	}, {
		name: "an element went away from the middle",
		doc:  list("a", "b", "c"),
		to:   list("a", "c"),
		want: "delete 1",
	}, {
		name: "an element went away from the front",
		doc:  list("a", "b", "c"),
		to:   list("b", "c"),
		want: "delete 0",
	}, {
		name: "an element went away from the end",
		doc:  list("a", "b", "c"),
		to:   list("a", "b"),
		want: "delete 2",
	}, {
		name: "an element joined in the middle",
		doc:  list("a", "c"),
		to:   list("a", "b", "c"),
		want: "insert 1 before 1",
	}, {
		name: "an element joined at the end",
		doc:  list("a", "b"),
		to:   list("a", "b", "c"),
		want: "insert 2 before 2",
	}, {
		name: "an element joined at the front",
		doc:  list("b", "c"),
		to:   list("a", "b", "c"),
		want: "insert 0 before 0",
	}, {
		// Two elements changed next to each other: pairing them keeps both
		// rewrites in place instead of deleting two lines and writing two.
		name: "neighbors both changed",
		doc:  list("a", "b", "c", "d"),
		to:   list("a", "x", "y", "d"),
		want: "rewrite 1 as 1; rewrite 2 as 2",
	}, {
		// More wanted than the document has, over the same stretch: what pairs
		// up is rewritten, and the rest joins before the next element that stays.
		name: "one element became two",
		doc:  list("a", "b", "d"),
		to:   list("a", "x", "y", "d"),
		want: "rewrite 1 as 1; insert 2 before 2",
	}, {
		name: "two elements became one",
		doc:  list("a", "b", "c", "d"),
		to:   list("a", "x", "d"),
		want: "rewrite 1 as 1; delete 2",
	}, {
		name: "the whole list is new",
		doc:  list("a"),
		to:   list("x", "y"),
		want: "rewrite 0 as 0; insert 1 before 1",
	}, {
		name: "the list was emptied",
		doc:  list("a", "b"),
		to:   nil,
		want: "delete 0; delete 1",
	}, {
		name: "an empty list filled up",
		doc:  nil,
		to:   list("a"),
		want: "insert 0 before 0",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := script(editalign.Elements(tc.doc, tc.to, equal))
			if got != tc.want {
				t.Errorf("Elements(%v, %v):\n got: %s\nwant: %s", tc.doc, tc.to, got, tc.want)
			}
		})
	}
}

// A list long enough to make the alignment table expensive is paired by position
// instead: an editor that has to choose between a slow edit and a plain one gets
// the plain one, and the ops still describe the same result.
func TestElements_FallsBackToPositionalForHugeLists(t *testing.T) {
	const n = 4000
	doc := make([]any, n)
	to := make([]any, n)
	for i := range doc {
		doc[i] = fmt.Sprint("host", i)
		to[i] = fmt.Sprint("other", i)
	}
	ops := editalign.Elements(doc, to, equal)
	if len(ops) != n {
		t.Fatalf("got %d ops, want %d", len(ops), n)
	}
	for i, op := range ops {
		if op.Kind != editalign.Rewrite || op.Doc != i || op.Want != i {
			t.Fatalf("ops[%d] = %+v, want a rewrite of %d as %d", i, op, i, i)
		}
	}
}

// Positional is the pairing an editor falls back to, and the shape it produces
// has to cover the ends of two lists of different length.
func TestPositional(t *testing.T) {
	for _, tc := range []struct {
		want string
		doc  int
		to   int
	}{
		{doc: 2, to: 2, want: "rewrite 0 as 0; rewrite 1 as 1"},
		{doc: 3, to: 1, want: "rewrite 0 as 0; delete 1; delete 2"},
		{doc: 1, to: 3, want: "rewrite 0 as 0; insert 1 before 1; insert 2 before 1"},
	} {
		t.Run(fmt.Sprintf("%d to %d", tc.doc, tc.to), func(t *testing.T) {
			got := script(editalign.Positional(tc.doc, tc.to))
			if got != tc.want {
				t.Errorf("Positional(%d, %d):\n got: %s\nwant: %s", tc.doc, tc.to, got, tc.want)
			}
		})
	}
}
