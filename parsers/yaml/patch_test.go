package yaml_test

import (
	"errors"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
)

// The parser advertises the interface, so kongfig.PatchDocument finds it.
var _ kongfig.DocumentPatcher = yamlparser.Parser{}

// patchYAML is the flow a program that shows a change before it writes it goes
// through: read the document, parse it, edit the data, ask for the edits instead
// of the document they make.
func patchYAML(t *testing.T, src string, edit func(kongfig.ConfigData)) (kongfig.DocumentPatch, error) {
	t.Helper()
	data, err := yamlparser.Default.Unmarshal([]byte(src))
	if err != nil {
		t.Fatal("parse the fixture:", err)
	}
	edit(data)
	return kongfig.PatchDocument(yamlparser.Default, []byte(src), data)
}

// A patch is the rewrite before it is written, so applying it has to give the
// document the rewrite gives.
func TestPatchDocument_AppliesToWhatTheEditorWrites(t *testing.T) {
	tests := map[string]struct {
		edit func(kongfig.ConfigData)
		src  string
	}{
		"one value changed": {src: nestedDoc, edit: func(d kongfig.ConfigData) {
			mapOf(t, d["server"])["dir"] = "/new"
		}},
		"a key added": {src: nestedDoc, edit: func(d kongfig.ConfigData) {
			mapOf(t, d["server"])["host"] = "localhost"
		}},
		"a key removed": {src: nestedDoc, edit: func(d kongfig.ConfigData) {
			delete(mapOf(t, d["server"]), "port")
		}},
		"an element appended": {src: listDoc, edit: func(d kongfig.ConfigData) {
			d["archive"] = append(seqOf(t, d["archive"]), "*.bak")
		}},
		"an element removed": {src: listDoc, edit: func(d kongfig.ConfigData) {
			d["archive"] = []any{"*.log"}
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			patch, err := patchYAML(t, tt.src, tt.edit)
			if err != nil {
				t.Fatal("patch:", err)
			}
			out, err := patch.Apply([]byte(tt.src))
			if err != nil {
				t.Fatal("apply:", err)
			}
			edited, err := editYAML(t, tt.src, tt.edit)
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
	patch, err := patchYAML(t, nestedDoc, func(d kongfig.ConfigData) {
		server := mapOf(t, d["server"])
		server["dir"] = "/new"
		server["port"] = 9090
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
		if edit.Start < 0 || edit.End > len(nestedDoc) || edit.End < edit.Start {
			t.Errorf("edit %d covers [%d,%d), which is not a range of the document", i, edit.Start, edit.End)
		}
		prev = edit.End
	}
	if covered := nestedDoc[patch.Edits[0].Start:patch.Edits[0].End]; covered != `"/old"` {
		t.Errorf("the first edit covers %q, want the old directory", covered)
	}
	if covered := nestedDoc[patch.Edits[1].Start:patch.Edits[1].End]; covered != "8080" {
		t.Errorf("the second edit covers %q, want the old port", covered)
	}
}

// A document that already holds the data needs no edit, and that is what a caller
// asking for a diff wants to hear.
func TestPatchDocument_IsEmptyForADocumentThatAlreadyHoldsTheData(t *testing.T) {
	patch, err := patchYAML(t, listDoc, func(kongfig.ConfigData) {})
	if err != nil {
		t.Fatal("patch:", err)
	}
	if len(patch.Edits) != 0 {
		t.Errorf("edits = %v, want none", patch.Edits)
	}
}

// A change the editor cannot make safely is refused the same way whether the
// caller asked for the document or for the edits.
func TestPatchDocument_RefusesWhatTheEditorRefuses(t *testing.T) {
	src := `motto: this text
  continues on the next line
`
	_, err := patchYAML(t, src, func(d kongfig.ConfigData) {
		d["motto"] = "changed"
	})
	if !errors.Is(err, yamlparser.ErrCannotEdit) {
		t.Errorf("err = %v, want ErrCannotEdit", err)
	}
}
