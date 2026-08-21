package kongfig_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// patchParser is lineParser with the edits exposed rather than applied: it
// replaces the text of a value that changed and removes the line of a key that
// went away, which is what the whole-document editor of lineParser does to the
// same input.
type patchParser struct{ lineParser }

func (patchParser) PatchDocument(src []byte, want kongfig.ConfigData) (kongfig.DocumentPatch, error) {
	var patch kongfig.DocumentPatch
	edited := map[string]bool{}
	at := 0
	for _, line := range strings.SplitAfter(string(src), "\n") {
		start := at
		at += len(line)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name := strings.TrimSpace(key)
		wanted, keep := want[name]
		if !keep {
			patch.Edits = append(patch.Edits, kongfig.DocumentEdit{Start: start, End: at})
			continue
		}
		edited[name] = true
		text := fmt.Sprint(wanted)
		if strings.TrimSpace(value) == text {
			continue
		}
		valueStart := start + len(key) + 1 + len(value) - len(strings.TrimLeft(value, " "))
		patch.Edits = append(patch.Edits, kongfig.DocumentEdit{
			Start: valueStart,
			End:   valueStart + len(strings.TrimSpace(value)),
			Text:  text,
		})
	}
	for name := range want {
		if !edited[name] {
			return kongfig.DocumentPatch{}, fmt.Errorf("no line for %q", name)
		}
	}
	return patch, nil
}

// sloppyPatcher hands back a patch for something other than what it was asked
// for — the mistake the check exists to catch.
type sloppyPatcher struct{ lineParser }

func (sloppyPatcher) PatchDocument(src []byte, _ kongfig.ConfigData) (kongfig.DocumentPatch, error) {
	return kongfig.DocumentPatch{Edits: []kongfig.DocumentEdit{
		{Start: len(src), End: len(src), Text: "sneaked = in\n"},
	}}, nil
}

// refusingPatcher stands for a change the format cannot express as an edit.
type refusingPatcher struct{ lineParser }

func (refusingPatcher) PatchDocument([]byte, kongfig.ConfigData) (kongfig.DocumentPatch, error) {
	return kongfig.DocumentPatch{}, errCannotExpress
}

const patchSrc = "# the port the server listens on\nport = 8080\nhost = localhost\n"

// A patch is the edit before it is written: apply it and the document is what
// the in-place editor would have returned.
func TestPatchDocument_AppliesToWhatTheEditorWouldWrite(t *testing.T) {
	want := kongfig.ConfigData{"port": "9090", "host": "localhost"}
	patch, err := kongfig.PatchDocument(patchParser{}, []byte(patchSrc), want)
	if err != nil {
		t.Fatal("patch:", err)
	}
	out, err := patch.Apply([]byte(patchSrc))
	if err != nil {
		t.Fatal("apply:", err)
	}
	edited, err := kongfig.EditDocument(patchParser{}, []byte(patchSrc), want)
	if err != nil {
		t.Fatal("edit:", err)
	}
	if !bytes.Equal(out, edited) {
		t.Errorf("patched document:\n got:\n%s\nwant:\n%s", out, edited)
	}
}

// The offsets are absolute and into the document as it was handed over, so a
// caller can show the text each edit covers without applying anything. They come
// back in ascending order, which is what a caller that walks the document
// alongside them needs.
func TestPatchDocument_ReportsAscendingAbsoluteOffsets(t *testing.T) {
	patch, err := kongfig.PatchDocument(patchParser{}, []byte(patchSrc),
		kongfig.ConfigData{"port": "9090"})
	if err != nil {
		t.Fatal("patch:", err)
	}
	if len(patch.Edits) != 2 {
		t.Fatalf("edits = %d, want 2 (the changed value and the dropped line)", len(patch.Edits))
	}
	prev := 0
	for i, edit := range patch.Edits {
		if edit.Start < prev {
			t.Errorf("edit %d starts at %d, behind the end of the one before it", i, edit.Start)
		}
		if edit.Start < 0 || edit.End > len(patchSrc) || edit.End < edit.Start {
			t.Errorf("edit %d covers [%d,%d), which is not a range of the document", i, edit.Start, edit.End)
		}
		prev = edit.End
	}
	if covered := patchSrc[patch.Edits[0].Start:patch.Edits[0].End]; covered != "8080" {
		t.Errorf("the first edit covers %q, want the old value", covered)
	}
	if covered := patchSrc[patch.Edits[1].Start:patch.Edits[1].End]; covered != "host = localhost\n" {
		t.Errorf("the second edit covers %q, want the dropped line", covered)
	}
}

// A document that already holds the data has nothing to change, and an empty
// patch is how the caller of a diff learns that.
func TestPatchDocument_IsEmptyForAnUnchangedDocument(t *testing.T) {
	patch, err := kongfig.PatchDocument(patchParser{}, []byte(patchSrc),
		kongfig.ConfigData{"port": "8080", "host": "localhost"})
	if err != nil {
		t.Fatal("patch:", err)
	}
	if len(patch.Edits) != 0 {
		t.Errorf("edits = %v, want none", patch.Edits)
	}
}

// A parser with an in-place editor but no patch is not an error in the data: the
// caller can still edit, it just cannot preview.
func TestPatchDocument_WithoutPatcher(t *testing.T) {
	patch, err := kongfig.PatchDocument(lineParser{}, []byte(patchSrc),
		kongfig.ConfigData{"port": "9090"})
	if !errors.Is(err, kongfig.ErrNoDocumentPatcher) {
		t.Errorf("err = %v, want ErrNoDocumentPatcher", err)
	}
	if len(patch.Edits) != 0 {
		t.Errorf("edits returned alongside the error: %v", patch.Edits)
	}
}

// The patch is checked the same way the rewrite is: a caller that shows a diff is
// showing what it will write, so a patch that writes the wrong thing must not
// reach the screen either.
func TestPatchDocument_VerifiesTheResult(t *testing.T) {
	patch, err := kongfig.PatchDocument(sloppyPatcher{}, []byte(patchSrc),
		kongfig.ConfigData{"port": "8080", "host": "localhost"})
	if err == nil {
		t.Fatalf("a patch that changes the wrong thing was accepted: %v", patch.Edits)
	}
	if !errors.Is(err, kongfig.ErrEditNotVerified) {
		t.Errorf("err = %v, want ErrEditNotVerified", err)
	}
	if len(patch.Edits) != 0 {
		t.Errorf("edits returned alongside the error: %v", patch.Edits)
	}
}

func TestPatchDocument_RefusedPatchKeepsTheReason(t *testing.T) {
	if _, err := kongfig.PatchDocument(refusingPatcher{}, []byte(patchSrc),
		kongfig.ConfigData{"port": "9090"}); !errors.Is(err, errCannotExpress) {
		t.Errorf("err = %v, want the parser's own error", err)
	}
}

func TestDocumentPatch_Apply(t *testing.T) {
	const src = "host = a\nport = 8080\n"
	tests := []struct {
		name  string
		want  string
		edits []kongfig.DocumentEdit
	}{
		{
			name:  "no edits leave the document as it is",
			edits: nil,
			want:  src,
		},
		{
			name:  "a replacement covers the old text",
			edits: []kongfig.DocumentEdit{{Start: 7, End: 8, Text: "b"}},
			want:  "host = b\nport = 8080\n",
		},
		{
			name:  "an empty span inserts",
			edits: []kongfig.DocumentEdit{{Start: 9, End: 9, Text: "name = yard\n"}},
			want:  "host = a\nname = yard\nport = 8080\n",
		},
		{
			name:  "empty text deletes",
			edits: []kongfig.DocumentEdit{{Start: 0, End: 9}},
			want:  "port = 8080\n",
		},
		{
			name: "edits go in wherever they sit, in any order",
			edits: []kongfig.DocumentEdit{
				{Start: 16, End: 20, Text: "9090"},
				{Start: 7, End: 8, Text: "b"},
			},
			want: "host = b\nport = 9090\n",
		},
		{
			name: "two edits at one offset keep the order they were given in",
			edits: []kongfig.DocumentEdit{
				{Start: 9, End: 9, Text: "one = 1\n"},
				{Start: 9, End: 9, Text: "two = 2\n"},
			},
			want: "host = a\none = 1\ntwo = 2\nport = 8080\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := kongfig.DocumentPatch{Edits: tt.edits}.Apply([]byte(src))
			if err != nil {
				t.Fatal("apply:", err)
			}
			if string(out) != tt.want {
				t.Errorf("patched document:\n got: %q\nwant: %q", out, tt.want)
			}
		})
	}
}

// Two edits over the same bytes have no order that gives both of them what they
// asked for, so the patch is refused rather than half-written.
func TestDocumentPatch_ApplyRefusesOverlappingEdits(t *testing.T) {
	const src = "host = a\nport = 8080\n"
	out, err := kongfig.DocumentPatch{Edits: []kongfig.DocumentEdit{
		{Start: 0, End: 9},
		{Start: 7, End: 8, Text: "b"},
	}}.Apply([]byte(src))
	if !errors.Is(err, kongfig.ErrPatchNotApplicable) {
		t.Errorf("err = %v, want ErrPatchNotApplicable", err)
	}
	if out != nil {
		t.Errorf("bytes returned alongside the error: %q", out)
	}
}

// A patch is a value a caller can build, hold on to, or read from somewhere else,
// so an edit that names bytes the document does not have is an error and never a
// panic.
func TestDocumentPatch_ApplyRefusesAnEditOutsideTheDocument(t *testing.T) {
	const src = "host = a\n"
	tests := map[string]kongfig.DocumentEdit{
		"before the start":     {Start: -1, End: 2},
		"past the end":         {Start: 4, End: len(src) + 1},
		"ending before it be":  {Start: 5, End: 3},
		"starting past theend": {Start: len(src) + 2, End: len(src) + 2, Text: "x"},
	}
	for name, edit := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := kongfig.DocumentPatch{Edits: []kongfig.DocumentEdit{edit}}.Apply([]byte(src))
			if !errors.Is(err, kongfig.ErrPatchNotApplicable) {
				t.Errorf("err = %v, want ErrPatchNotApplicable", err)
			}
			if out != nil {
				t.Errorf("bytes returned alongside the error: %q", out)
			}
		})
	}
}

// Apply reads the document, it does not write to it: a caller can apply the same
// patch to the same bytes twice, or show a preview and then write.
func TestDocumentPatch_ApplyLeavesTheSourceAlone(t *testing.T) {
	src := []byte("host = a\n")
	patch := kongfig.DocumentPatch{Edits: []kongfig.DocumentEdit{{Start: 7, End: 8, Text: "b"}}}
	if _, err := patch.Apply(src); err != nil {
		t.Fatal("apply:", err)
	}
	if string(src) != "host = a\n" {
		t.Errorf("the source changed under the caller: %q", src)
	}
	out, err := patch.Apply(src)
	if err != nil {
		t.Fatal("apply again:", err)
	}
	if string(out) != "host = b\n" {
		t.Errorf("second apply gave %q", out)
	}
}
