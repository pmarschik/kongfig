package yaml_test

import (
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// A list element carries the comment written beside it. Pairing the elements of
// the edited list with the elements of the document by position moves every
// comment below the edit onto a different value, which is the one thing an
// in-place edit must never do.
const commentedListDoc = `hosts:
  - alpha # primary
  - beta # secondary
  - gamma # tertiary
`

// Taking an element out of the middle takes out its line. The elements below it
// keep their text, and with it the comments their author wrote.
func TestEditDocument_RemovesSequenceElementFromTheMiddle(t *testing.T) {
	got := mustEditYAML(t, commentedListDoc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"alpha", "gamma"}
	})
	want := `hosts:
  - alpha # primary
  - gamma # tertiary
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Taking the first element out is the same edit read from the other end.
func TestEditDocument_RemovesFirstSequenceElement(t *testing.T) {
	got := mustEditYAML(t, commentedListDoc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"beta", "gamma"}
	})
	want := `hosts:
  - beta # secondary
  - gamma # tertiary
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A new element goes in where the list says it goes, not at the end, and the
// elements around it are left alone.
func TestEditDocument_InsertsSequenceElementInTheMiddle(t *testing.T) {
	got := mustEditYAML(t, commentedListDoc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"alpha", "beta", "delta", "gamma"}
	})
	want := `hosts:
  - alpha # primary
  - beta # secondary
  - delta
  - gamma # tertiary
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An element whose value changed is rewritten where it is written, so the
// comment beside it stays beside it. This is what pairing by position already
// gets right, and what matching by identity must keep getting right.
func TestEditDocument_RewritesTheSequenceElementThatChanged(t *testing.T) {
	got := mustEditYAML(t, commentedListDoc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"alpha", "beta.example", "gamma"}
	})
	want := `hosts:
  - alpha # primary
  - beta.example # secondary
  - gamma # tertiary
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A comment written above an element is about that element. It goes away with
// the element, rather than staying behind to describe whatever moves up into its
// place.
const commentAboveListDoc = `hosts:
  - alpha
  # the fallback
  - beta
  - gamma
`

func TestEditDocument_RemovesTheCommentAboveTheElementItRemoves(t *testing.T) {
	got := mustEditYAML(t, commentAboveListDoc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"alpha", "gamma"}
	})
	want := `hosts:
  - alpha
  - gamma
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// For the same reason a new element goes in above that comment: wedging it
// between the comment and the element would hand it a description of its
// neighbor.
func TestEditDocument_InsertsAboveTheCommentOfTheElementItPrecedes(t *testing.T) {
	got := mustEditYAML(t, commentAboveListDoc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"alpha", "delta", "beta", "gamma"}
	})
	want := `hosts:
  - alpha
  - delta
  # the fallback
  - beta
  - gamma
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A list on one line keeps the quoting of the elements that stay, wherever the
// element that goes away sat.
func TestEditDocument_EditsFlowSequenceInTheMiddle(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
		to   []any
	}{
		{
			name: "remove the middle",
			src:  `tags: ["a", "b", "c"]` + "\n",
			to:   []any{"a", "c"},
			want: `tags: ["a", "c"]` + "\n",
		},
		{
			name: "insert in the middle",
			src:  `tags: ["a", "c"]` + "\n",
			to:   []any{"a", "b", "c"},
			want: `tags: ["a", "b", "c"]` + "\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mustEditYAML(t, tc.src, func(d kongfig.ConfigData) {
				d["tags"] = tc.to
			})
			if got != tc.want {
				t.Errorf("edited document:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}
