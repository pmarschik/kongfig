package editsplice_test

import (
	"testing"

	"github.com/pmarschik/kongfig/internal/editsplice"
)

const src = "host = a\nport = 8080\nname = yard\n"

func TestApply(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  string
		edits []editsplice.Edit
	}{
		{
			name:  "no edits copy the document",
			edits: nil,
			want:  src,
		},
		{
			name:  "a replacement takes the span it names",
			edits: []editsplice.Edit{{Start: 16, End: 20, Text: "9090"}},
			want:  "host = a\nport = 9090\nname = yard\n",
		},
		{
			name:  "an empty span inserts",
			edits: []editsplice.Edit{{Start: 9, End: 9, Text: "# the port\n"}},
			want:  "host = a\n# the port\nport = 8080\nname = yard\n",
		},
		{
			name:  "empty text deletes",
			edits: []editsplice.Edit{{Start: 9, End: 21}},
			want:  "host = a\nname = yard\n",
		},
		{
			// The editors collect edits as they walk the document, and a walk
			// arrives at the text in no fixed order.
			name: "the order the edits arrive in does not matter",
			edits: []editsplice.Edit{
				{Start: 28, End: 32, Text: "yard.example"},
				{Start: 7, End: 8, Text: "a.example"},
				{Start: 16, End: 20, Text: "9090"},
			},
			want: "host = a.example\nport = 9090\nname = yard.example\n",
		},
		{
			name: "two edits meeting at one offset both go in",
			edits: []editsplice.Edit{
				{Start: 9, End: 9, Text: "# first\n"},
				{Start: 9, End: 9, Text: "# second\n"},
			},
			want: "host = a\n# first\n# second\nport = 8080\nname = yard\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := editsplice.Apply([]byte(src), tc.edits)
			if !ok {
				t.Fatal("the edits were refused as overlapping")
			}
			if string(got) != tc.want {
				t.Errorf("spliced document:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// Two edits over the same bytes would each rewrite what the other read. There
// is no order that gives both what they asked for, so neither one goes in.
func TestApply_RefusesOverlappingEdits(t *testing.T) {
	_, ok := editsplice.Apply([]byte(src), []editsplice.Edit{
		{Start: 9, End: 21, Text: ""},
		{Start: 16, End: 20, Text: "9090"},
	})
	if ok {
		t.Error("overlapping edits were accepted")
	}
}

// The document the caller passed in is the document it keeps: the result is a
// new buffer, even when there is nothing to change.
func TestApply_LeavesTheSourceAlone(t *testing.T) {
	in := []byte(src)
	out, ok := editsplice.Apply(in, []editsplice.Edit{{Start: 0, End: 4, Text: "HOST"}})
	if !ok {
		t.Fatal("the edit was refused")
	}
	out[0] = 'x'
	if string(in) != src {
		t.Errorf("source document changed: %q", in)
	}
}

// The whole point of splicing in one pass: the document is copied once, not once
// per edit, so the allocation count does not grow with the number of edits.
func TestApply_AllocatesTheSameForOneEditAndForMany(t *testing.T) {
	one := []editsplice.Edit{{Start: 16, End: 20, Text: "9090"}}
	many := make([]editsplice.Edit, 0, 8)
	for i := range 8 {
		many = append(many, editsplice.Edit{Start: i, End: i + 1, Text: "x"})
	}
	in := []byte(src)
	allocs := func(edits []editsplice.Edit) float64 {
		return testing.AllocsPerRun(100, func() { editsplice.Apply(in, edits) })
	}
	if got, want := allocs(many), allocs(one); got > want {
		t.Errorf("8 edits allocate %v, one edit allocates %v", got, want)
	}
}
