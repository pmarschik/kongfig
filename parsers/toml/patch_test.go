package toml_test

import (
	"errors"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

// The parser advertises the interface, so kongfig.PatchDocument finds it.
var _ kongfig.DocumentPatcher = tomlparser.Parser{}

// patchTOML is the flow a program that shows a change before it writes it goes
// through: read the document, parse it, edit the data, ask for the edits instead
// of the document they make.
func patchTOML(t *testing.T, src string, edit func(kongfig.ConfigData)) (kongfig.DocumentPatch, error) {
	t.Helper()
	data, err := tomlparser.Default.Unmarshal([]byte(src))
	if err != nil {
		t.Fatal("parse the fixture:", err)
	}
	edit(data)
	return kongfig.PatchDocument(tomlparser.Default, []byte(src), data)
}

// A patch is the rewrite before it is written, so applying it has to give the
// document the rewrite gives.
func TestPatchDocument_AppliesToWhatTheEditorWrites(t *testing.T) {
	tests := map[string]struct {
		edit func(kongfig.ConfigData)
		src  string
	}{
		"one value changed": {src: sectionSrc, edit: func(d kongfig.ConfigData) {
			tableOf(t, d["server"])["port"] = int64(9090)
		}},
		"a root key added": {src: sectionSrc, edit: func(d kongfig.ConfigData) {
			d["name"] = "yard"
		}},
		"a key removed": {src: sectionSrc, edit: func(d kongfig.ConfigData) {
			delete(d, "version")
		}},
		"an element appended": {src: arraySrc, edit: func(d kongfig.ConfigData) {
			d["archive"] = append(toAnySlice(d["archive"]), "*.bak")
		}},
		"an element removed": {src: arraySrc, edit: func(d kongfig.ConfigData) {
			d["archive"] = []any{"*.tmp"}
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			patch, err := patchTOML(t, tt.src, tt.edit)
			if err != nil {
				t.Fatal("patch:", err)
			}
			out, err := patch.Apply([]byte(tt.src))
			if err != nil {
				t.Fatal("apply:", err)
			}
			edited, err := editTOML(t, tt.src, tt.edit)
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
	patch, err := patchTOML(t, sectionSrc, func(d kongfig.ConfigData) {
		d["version"] = int64(2)
		tableOf(t, d["server"])["port"] = int64(9090)
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
		if edit.Start < 0 || edit.End > len(sectionSrc) || edit.End < edit.Start {
			t.Errorf("edit %d covers [%d,%d), which is not a range of the document", i, edit.Start, edit.End)
		}
		prev = edit.End
	}
	if covered := sectionSrc[patch.Edits[0].Start:patch.Edits[0].End]; covered != "1" {
		t.Errorf("the first edit covers %q, want the old version", covered)
	}
	if covered := sectionSrc[patch.Edits[1].Start:patch.Edits[1].End]; covered != "8080" {
		t.Errorf("the second edit covers %q, want the old port", covered)
	}
}

// A document that already holds the data needs no edit, and that is what a caller
// asking for a diff wants to hear.
func TestPatchDocument_IsEmptyForADocumentThatAlreadyHoldsTheData(t *testing.T) {
	patch, err := patchTOML(t, arraySrc, func(kongfig.ConfigData) {})
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
	_, err := patchTOML(t, sectionSrc, func(d kongfig.ConfigData) {
		d["db"] = kongfig.ConfigData{"host": "localhost"}
	})
	if !errors.Is(err, tomlparser.ErrCannotEdit) {
		t.Errorf("err = %v, want ErrCannotEdit", err)
	}
}
