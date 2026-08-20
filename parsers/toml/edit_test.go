package toml_test

import (
	"errors"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

// editTOML is the flow a program that changes a config file goes through: read
// the document, parse it, edit the data, write the bytes that come back. The
// rewrite runs through kongfig.EditDocument, so every case here also asserts
// that the result parses back to the data it asked for.
func editTOML(t *testing.T, src string, edit func(kongfig.ConfigData)) (string, error) {
	t.Helper()
	data, err := tomlparser.Default.Unmarshal([]byte(src))
	if err != nil {
		t.Fatal("parse the fixture:", err)
	}
	edit(data)
	out, err := kongfig.EditDocument(tomlparser.Default, []byte(src), data)
	return string(out), err
}

// mustEdit fails the test when the edit is refused.
func mustEdit(t *testing.T, src string, edit func(kongfig.ConfigData)) string {
	t.Helper()
	out, err := editTOML(t, src, edit)
	if err != nil {
		t.Fatal("edit:", err)
	}
	return out
}

const arraySrc = `# what to archive
archive = [
  # logs pile up
  "*.log",
  "*.tmp",
]
`

// Appending to a list is the edit a program most often has to make, and the
// element it adds joins the layout the list already uses — one per line, with the
// indentation and the trailing comma of its neighbors.
func TestEditDocument_AppendsArrayElement(t *testing.T) {
	got := mustEdit(t, arraySrc, func(d kongfig.ConfigData) {
		d["archive"] = append(toAnySlice(d["archive"]), "*.bak")
	})
	want := `# what to archive
archive = [
  # logs pile up
  "*.log",
  "*.tmp",
  "*.bak",
]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Removing an element takes out one line, not the rest of the list: the comments
// the author wrote stay where they wrote them.
func TestEditDocument_RemovesArrayElement(t *testing.T) {
	got := mustEdit(t, arraySrc, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.log"}
	})
	want := `# what to archive
archive = [
  # logs pile up
  "*.log",
]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A list written on one line stays on one line, and an empty one is where a
// program that maintains a list starts.
func TestEditDocument_AppendsToOneLineArray(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "one element",
			src:  "tags = [\"a\"]\n",
			want: "tags = [\"a\", \"b\"]\n",
		},
		{
			name: "empty",
			src:  "tags = []\n",
			want: "tags = [\"b\"]\n",
		},
		{
			name: "spaced",
			src:  "tags = [ \"a\" ]\n",
			want: "tags = [ \"a\", \"b\" ]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mustEdit(t, tc.src, func(d kongfig.ConfigData) {
				d["tags"] = append(toAnySlice(d["tags"]), "b")
			})
			if got != tc.want {
				t.Errorf("edited document:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// The same data edit reaches the same value whether the document spells a list of
// tables as a run of [[blocks]] or as one line of inline tables — the caller does
// not know which, and does not have to.
func TestEditDocument_RewritesScalarInEitherTableArrayLayout(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "blocks",
			src: `[[aux]]
dir = "old"   # where it lives
parent = "p"

[[aux]]
dir = "other"
`,
			want: `[[aux]]
dir = "new"   # where it lives
parent = "p"

[[aux]]
dir = "other"
`,
		},
		{
			name: "inline",
			src:  "aux = [{dir = \"old\", parent = \"p\"}, {dir = \"other\"}]\n",
			want: "aux = [{dir = \"new\", parent = \"p\"}, {dir = \"other\"}]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mustEdit(t, tc.src, func(d kongfig.ConfigData) {
				tableIn(t, d["aux"], 0)["dir"] = "new"
			})
			if got != tc.want {
				t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

const sectionSrc = `version = 1

[server]
port = 8080
`

// A new key belongs to the table it is written under, so a top-level key goes
// ahead of the first header — after one it would belong to that section.
func TestEditDocument_InsertsRootKeyBeforeTheFirstHeader(t *testing.T) {
	got := mustEdit(t, sectionSrc, func(d kongfig.ConfigData) {
		d["name"] = "yard"
	})
	want := `version = 1
name = "yard"

[server]
port = 8080
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A new key inside a section joins that section, after the keys already there.
func TestEditDocument_InsertsKeyIntoItsTable(t *testing.T) {
	got := mustEdit(t, sectionSrc, func(d kongfig.ConfigData) {
		tableOf(t, d["server"])["host"] = "localhost"
	})
	want := `version = 1

[server]
port = 8080
host = "localhost"
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A dropped key takes its line and nothing else.
func TestEditDocument_DeletesKeyLine(t *testing.T) {
	got := mustEdit(t, sectionSrc, func(d kongfig.ConfigData) {
		delete(d, "version")
	})
	want := `
[server]
port = 8080
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Syntax inside a string is text, not syntax: a scanner that read the "#" in a
// value as a comment, or the "=" in it as a second assignment, would map the
// wrong bytes to the key and rewrite the wrong part of the line.
func TestEditDocument_ReadsPastStringLiterals(t *testing.T) {
	src := `# a comment with = and [brackets]
motto = "keys = values # really"
path = 'C:\dir[0]'
note = """
a = b
"""
port = 8080
`
	got := mustEdit(t, src, func(d kongfig.ConfigData) {
		d["port"] = 9090
	})
	want := strings.Replace(src, "port = 8080", "port = 9090", 1)
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A dotted key is one key with a path, and editing its value leaves the spelling
// the author chose alone.
func TestEditDocument_RewritesDottedKey(t *testing.T) {
	src := "server.port = 8080\n"
	got := mustEdit(t, src, func(d kongfig.ConfigData) {
		tableOf(t, d["server"])["port"] = 9090
	})
	if want := "server.port = 9090\n"; got != want {
		t.Errorf("edited document:\n got: %q\nwant: %q", got, want)
	}
}

// An edit the format cannot express as an edit is refused, rather than guessed
// at: the caller hears about it and can fall back to a full rewrite.
func TestEditDocument_RefusesWhatItCannotExpress(t *testing.T) {
	if _, err := editTOML(t, sectionSrc, func(d kongfig.ConfigData) {
		d["db"] = kongfig.ConfigData{"host": "localhost"}
	}); err == nil {
		t.Error("adding a section the document does not have was accepted")
	} else if errors.Is(err, kongfig.ErrNoDocumentEditor) {
		t.Errorf("err = %v, want a refusal from the editor", err)
	}
}

// An edit that changes nothing returns the document unchanged, byte for byte.
func TestEditDocument_NoChangeIsNoEdit(t *testing.T) {
	got := mustEdit(t, arraySrc, func(kongfig.ConfigData) {})
	if got != arraySrc {
		t.Errorf("editing nothing changed the document:\n got:\n%s\nwant:\n%s", got, arraySrc)
	}
}

// The parser advertises the interface, so kongfig.EditDocument finds it.
var _ kongfig.DocumentEditor = tomlparser.Parser{}

// tableIn reaches the i-th table of a parsed list of tables. The decoder reports
// a list of [[blocks]] as []map[string]any and a list of inline tables as []any,
// so a test that has to work on either layout goes through here.
func tableIn(t *testing.T, list any, i int) map[string]any {
	t.Helper()
	switch l := list.(type) {
	case []map[string]any:
		return l[i]
	case []any:
		return tableOf(t, l[i])
	}
	t.Fatalf("no table at index %d of %T", i, list)
	return nil
}

// tableOf reaches a parsed table, whichever map type it came back as.
func tableOf(t *testing.T, v any) map[string]any {
	t.Helper()
	switch m := v.(type) {
	case kongfig.ConfigData:
		return m
	case map[string]any:
		return m
	}
	t.Fatalf("not a table: %T", v)
	return nil
}

// toAnySlice widens a parsed list so a test can append to it.
func toAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}
