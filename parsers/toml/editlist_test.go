package toml_test

import (
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// A list element carries the comment written beside it. Pairing the elements of
// the edited list with the elements of the document by position moves every
// comment below the edit onto a different value, which is the one thing an
// in-place edit must never do.
const commentedArraySrc = `hosts = [
  "alpha", # primary
  "beta", # secondary
  "gamma", # tertiary
]
`

// Taking an element out of the middle takes out its line. The elements below it
// keep their text, and with it the comments their author wrote.
func TestEditDocument_RemovesArrayElementFromTheMiddle(t *testing.T) {
	got := mustEdit(t, commentedArraySrc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"alpha", "gamma"}
	})
	want := `hosts = [
  "alpha", # primary
  "gamma", # tertiary
]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Taking the first element out is the same edit read from the other end.
func TestEditDocument_RemovesFirstArrayElement(t *testing.T) {
	got := mustEdit(t, commentedArraySrc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"beta", "gamma"}
	})
	want := `hosts = [
  "beta", # secondary
  "gamma", # tertiary
]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A new element goes in where the list says it goes, not at the end, and it
// joins the layout the list already uses.
func TestEditDocument_InsertsArrayElementInTheMiddle(t *testing.T) {
	got := mustEdit(t, commentedArraySrc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"alpha", "beta", "delta", "gamma"}
	})
	want := `hosts = [
  "alpha", # primary
  "beta", # secondary
  "delta",
  "gamma", # tertiary
]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An element whose value changed is rewritten where it is written, so the
// comment beside it stays beside it.
func TestEditDocument_RewritesTheArrayElementThatChanged(t *testing.T) {
	got := mustEdit(t, commentedArraySrc, func(d kongfig.ConfigData) {
		d["hosts"] = []any{"alpha", "beta.example", "gamma"}
	})
	want := `hosts = [
  "alpha", # primary
  "beta.example", # secondary
  "gamma", # tertiary
]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A list on one line takes the same edits without leaving its line.
func TestEditDocument_EditsOneLineArrayInTheMiddle(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
		to   []any
	}{
		{
			name: "remove the middle",
			src:  "tags = [\"a\", \"b\", \"c\"]\n",
			to:   []any{"a", "c"},
			want: "tags = [\"a\", \"c\"]\n",
		},
		{
			name: "insert in the middle",
			src:  "tags = [\"a\", \"c\"]\n",
			to:   []any{"a", "b", "c"},
			want: "tags = [\"a\", \"b\", \"c\"]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mustEdit(t, tc.src, func(d kongfig.ConfigData) {
				d["tags"] = tc.to
			})
			if got != tc.want {
				t.Errorf("edited document:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// A comment written above an element is about that element. It goes away with
// the element, rather than staying behind to describe whatever moves up into its
// place — and a new element goes in above it rather than between the two.
func TestEditDocument_TakesTheCommentAboveTheElementItRemoves(t *testing.T) {
	got := mustEdit(t, arraySrc, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.tmp"}
	})
	want := `# what to archive
archive = [
  "*.tmp",
]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_InsertsAboveTheCommentOfTheElementItPrecedes(t *testing.T) {
	got := mustEdit(t, arraySrc, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.new", "*.log", "*.tmp"}
	})
	want := `# what to archive
archive = [
  "*.new",
  # logs pile up
  "*.log",
  "*.tmp",
]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A list of sections is a list too: dropping one of them takes that section's
// lines, and leaves the sections around it — and their comments — as they were.
const blockArraySrc = `[[aux]]
dir = "a" # the first one

[[aux]]
dir = "b" # the second one

[[aux]]
dir = "c" # the third one
`

func TestEditDocument_RemovesTableArrayElementFromTheMiddle(t *testing.T) {
	got := mustEdit(t, blockArraySrc, func(d kongfig.ConfigData) {
		d["aux"] = []any{
			map[string]any{"dir": "a"},
			map[string]any{"dir": "c"},
		}
	})
	want := `[[aux]]
dir = "a" # the first one

[[aux]]
dir = "c" # the third one
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A section the document would have to grow is still refused, wherever in the
// list it would go: a refusal the caller can fall back from beats a guess at
// where a new [[block]] belongs.
func TestEditDocument_RefusesANewTableArrayElementInTheMiddle(t *testing.T) {
	_, err := editTOML(t, blockArraySrc, func(d kongfig.ConfigData) {
		d["aux"] = []any{
			map[string]any{"dir": "a"},
			map[string]any{"dir": "b"},
			map[string]any{"dir": "new"},
			map[string]any{"dir": "c"},
		}
	})
	if err == nil {
		t.Error("writing a new section into the middle of a list of sections was accepted")
	}
}
