package json_test

import (
	"errors"
	"math"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
)

var _ kongfig.DocumentPatcher = jsonparser.Parser{}

// patchJSON is the flow a program that shows a change before it writes it goes
// through: read the document, parse it, edit the data, ask for the edits instead
// of the document they make.
func patchJSON(t *testing.T, src string, edit func(kongfig.ConfigData)) (kongfig.DocumentPatch, error) {
	t.Helper()
	data, err := jsonparser.Default.Unmarshal([]byte(src))
	if err != nil {
		t.Fatal("parse the fixture:", err)
	}
	edit(data)
	return kongfig.PatchDocument(jsonparser.Default, []byte(src), data)
}

// A patch is the rewrite before it is written, so applying it has to give the
// document the rewrite gives — for the layouts an editor finds hardest as much as
// for a value swapped in place.
func TestPatchDocument_AppliesToWhatTheEditorWrites(t *testing.T) {
	tests := map[string]struct {
		edit func(kongfig.ConfigData)
		src  string
	}{
		"one value changed": {src: objectSrc, edit: func(d kongfig.ConfigData) {
			d["port"] = float64(9090)
		}},
		"a key added": {src: objectSrc, edit: func(d kongfig.ConfigData) {
			d["timeout"] = float64(30)
		}},
		"a key removed": {src: objectSrc, edit: func(d kongfig.ConfigData) {
			delete(d, "host")
		}},
		"a key renamed": {src: objectSrc, edit: func(d kongfig.ConfigData) {
			delete(d, "port")
			d["listen"] = float64(8080)
		}},
		"a key two levels down": {src: objectSrc, edit: func(d kongfig.ConfigData) {
			asObject(t, d["db"])["user"] = "admin"
		}},
		"an element appended": {src: arraySrc, edit: func(d kongfig.ConfigData) {
			d["archive"] = append(asList(t, d["archive"]), "*.bak")
		}},
		"every element replaced": {src: arraySrc, edit: func(d kongfig.ConfigData) {
			d["archive"] = []any{"*.bak"}
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			patch, err := patchJSON(t, tt.src, tt.edit)
			if err != nil {
				t.Fatal("patch:", err)
			}
			out, err := patch.Apply([]byte(tt.src))
			if err != nil {
				t.Fatal("apply:", err)
			}
			edited, err := editJSON(t, jsonparser.Default, tt.src, tt.edit)
			if err != nil {
				t.Fatal("edit:", err)
			}
			if string(out) != edited {
				t.Errorf("patched document:\n got:\n%s\nwant:\n%s", out, edited)
			}
		})
	}
}

// The offsets are into the document as it was handed over, in ascending order, so
// a caller can cut the text each edit covers out of it without applying anything.
func TestPatchDocument_ReportsAscendingOffsetsIntoTheSource(t *testing.T) {
	patch, err := patchJSON(t, objectSrc, func(d kongfig.ConfigData) {
		d["port"] = float64(9090)
		asObject(t, d["db"])["name"] = "otherdb"
	})
	if err != nil {
		t.Fatal("patch:", err)
	}
	if len(patch.Edits) != 2 {
		t.Fatalf("edits = %d, want one per changed value", len(patch.Edits))
	}
	prev := 0
	for i, edit := range patch.Edits {
		if edit.Start < prev {
			t.Errorf("edit %d starts at %d, behind the end of the one before it", i, edit.Start)
		}
		if edit.Start < 0 || edit.End > len(objectSrc) || edit.End < edit.Start {
			t.Errorf("edit %d covers [%d,%d), which is not a range of the document", i, edit.Start, edit.End)
		}
		prev = edit.End
	}
	if covered := objectSrc[patch.Edits[0].Start:patch.Edits[0].End]; covered != "8080" {
		t.Errorf("the first edit covers %q, want the old port", covered)
	}
	if covered := objectSrc[patch.Edits[1].Start:patch.Edits[1].End]; covered != `"mydb"` {
		t.Errorf("the second edit covers %q, want the old database name", covered)
	}
}

// A document that already holds the data needs no edit, and that is what a caller
// asking for a diff wants to hear.
func TestPatchDocument_IsEmptyForADocumentThatAlreadyHoldsTheData(t *testing.T) {
	patch, err := patchJSON(t, objectSrc, func(kongfig.ConfigData) {})
	if err != nil {
		t.Fatal("patch:", err)
	}
	if len(patch.Edits) != 0 {
		t.Errorf("edits = %v, want none", patch.Edits)
	}
}

// A change the format cannot express is refused the same way whether the caller
// asked for the document or for the edits.
func TestPatchDocument_RefusesWhatTheEditorRefuses(t *testing.T) {
	src := `{"port": 8080}`
	data, err := jsonparser.Default.Unmarshal([]byte(src))
	if err != nil {
		t.Fatal("parse the fixture:", err)
	}
	data["ratio"] = math.NaN()
	if _, err := kongfig.PatchDocument(jsonparser.Default, []byte(src), data); !errors.Is(err, jsonparser.ErrCannotEdit) {
		t.Errorf("err = %v, want ErrCannotEdit", err)
	}
}
