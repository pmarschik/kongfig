package yaml_test

import (
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
)

// editYAML is the flow a program that changes a config file goes through: read
// the document, parse it, edit the data, write the bytes that come back. The
// rewrite runs through kongfig.EditDocument, so every case here also asserts that
// the result parses back to the data it asked for.
func editYAML(t *testing.T, src string, edit func(kongfig.ConfigData)) (string, error) {
	t.Helper()
	data, err := yamlparser.Default.Unmarshal([]byte(src))
	if err != nil {
		t.Fatal("parse the fixture:", err)
	}
	edit(data)
	out, err := kongfig.EditDocument(yamlparser.Default, []byte(src), data)
	return string(out), err
}

// mustEditYAML fails the test when the edit is refused.
func mustEditYAML(t *testing.T, src string, edit func(kongfig.ConfigData)) string {
	t.Helper()
	out, err := editYAML(t, src, edit)
	if err != nil {
		t.Fatal("edit:", err)
	}
	return out
}

const listDoc = `# what to archive
archive:
  # logs pile up
  - "*.log"
  - "*.tmp"
name: yard
`

// Appending to a list is the edit a program most often has to make, and the
// element it adds joins the list where it is, indented like the ones above it —
// not at the end of the document, and not with the rest of the file reflowed.
func TestEditDocument_AppendsSequenceElement(t *testing.T) {
	got := mustEditYAML(t, listDoc, func(d kongfig.ConfigData) {
		d["archive"] = append(seqOf(t, d["archive"]), "*.bak")
	})
	want := `# what to archive
archive:
  # logs pile up
  - "*.log"
  - "*.tmp"
  - "*.bak"
name: yard
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Removing an element takes out one line, and the comments the author wrote stay
// where they wrote them.
func TestEditDocument_RemovesSequenceElement(t *testing.T) {
	got := mustEditYAML(t, listDoc, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.log"}
	})
	want := `# what to archive
archive:
  # logs pile up
  - "*.log"
name: yard
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A list written on one line stays on one line.
func TestEditDocument_AppendsToFlowSequence(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{name: "one element", src: "tags: [a]\n", want: "tags: [a, b]\n"},
		{name: "empty", src: "tags: []\n", want: "tags: [b]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mustEditYAML(t, tc.src, func(d kongfig.ConfigData) {
				d["tags"] = append(seqOf(t, d["tags"]), "b")
			})
			if got != tc.want {
				t.Errorf("edited document:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

const nestedDoc = `server:
  # where it lives
  dir: "/old"   # the old one
  port: 8080
`

// A nested value is rewritten where it is written, keeping the quoting the author
// chose and the comment they put after it.
func TestEditDocument_RewritesNestedScalar(t *testing.T) {
	got := mustEditYAML(t, nestedDoc, func(d kongfig.ConfigData) {
		mapOf(t, d["server"])["dir"] = "/new"
	})
	want := `server:
  # where it lives
  dir: "/new"   # the old one
  port: 8080
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A new key joins the mapping it belongs to, indented with the keys already
// there — not appended to the document.
func TestEditDocument_InsertsKeyIntoItsMapping(t *testing.T) {
	got := mustEditYAML(t, nestedDoc, func(d kongfig.ConfigData) {
		mapOf(t, d["server"])["host"] = "localhost"
	})
	want := `server:
  # where it lives
  dir: "/old"   # the old one
  port: 8080
  host: localhost
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A dropped key takes its line and nothing else.
func TestEditDocument_DeletesKeyLine(t *testing.T) {
	got := mustEditYAML(t, nestedDoc, func(d kongfig.ConfigData) {
		delete(mapOf(t, d["server"]), "port")
	})
	want := `server:
  # where it lives
  dir: "/old"   # the old one
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A block scalar is text, not structure: an editor that read the "key:" inside
// one as a key, or the "#" as a comment, would rewrite the wrong bytes.
func TestEditDocument_ReadsPastBlockScalars(t *testing.T) {
	src := `motd: |
  port: 1234
  # not a comment
port: 8080
`
	got := mustEditYAML(t, src, func(d kongfig.ConfigData) {
		d["port"] = 9090
	})
	want := strings.Replace(src, "port: 8080", "port: 9090", 1)
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An edit the editor cannot make safely is refused, rather than guessed at: the
// caller hears about it and can fall back to a full rewrite.
func TestEditDocument_RefusesWhatItCannotExpress(t *testing.T) {
	src := `motto: this text
  continues on the next line
`
	if _, err := editYAML(t, src, func(d kongfig.ConfigData) {
		d["motto"] = "changed"
	}); err == nil {
		t.Error("rewriting a value spread over several lines was accepted")
	} else if strings.Contains(err.Error(), kongfig.ErrNoDocumentEditor.Error()) {
		t.Errorf("err = %v, want a refusal from the editor", err)
	}
}

// An edit that changes nothing returns the document unchanged, byte for byte.
func TestEditDocument_NoChangeIsNoEdit(t *testing.T) {
	got := mustEditYAML(t, listDoc, func(kongfig.ConfigData) {})
	if got != listDoc {
		t.Errorf("editing nothing changed the document:\n got:\n%s\nwant:\n%s", got, listDoc)
	}
}

// mapOf reaches a parsed mapping, whichever map type it came back as.
func mapOf(t *testing.T, v any) map[string]any {
	t.Helper()
	switch m := v.(type) {
	case kongfig.ConfigData:
		return m
	case map[string]any:
		return m
	}
	t.Fatalf("not a mapping: %T", v)
	return nil
}

// seqOf reaches a parsed list, so a test can append to it.
func seqOf(t *testing.T, v any) []any {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("not a list: %T", v)
	}
	return list
}

// The parser advertises the interface, so kongfig.EditDocument finds it.
var _ kongfig.DocumentEditor = yamlparser.Parser{}
