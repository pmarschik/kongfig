package toml_test

import (
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

// A list of tables is the one value whose default written form is a key/value
// line rather than a section: [Parser.Marshal] writes small elements inline. A
// new key holding one is therefore a line the editor can write, and refusing it
// would send the caller to Marshal for a change the format expresses fine.

const noListSrc = `# where the checkouts live
dirname = "."

[buckets.all]
match = ["@"]
`

func TestEditDocument_WritesANewTableArrayAsAKeyValueLine(t *testing.T) {
	got := mustEdit(t, noListSrc, func(d kongfig.ConfigData) {
		d["rewrites"] = []any{map[string]any{"match": "forge@org/repo", "bucket": "work"}}
	})
	want := `# where the checkouts live
dirname = "."
rewrites = [{bucket = "work", match = "forge@org/repo"}]

[buckets.all]
match = ["@"]
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// The limit that governs the written form governs the edit too: an element with
// more keys than may be inlined is a [[section]] when Marshal writes it, and a
// section is what the editor has no place for.
func TestEditDocument_RefusesANewTableArrayTooBigToInline(t *testing.T) {
	_, err := editTOML(t, noListSrc, func(d kongfig.ConfigData) {
		d["rewrites"] = []any{map[string]any{
			"match": "forge@org/repo", "bucket": "work", "org": "org", "repo": "repo",
		}}
	})
	if err == nil {
		t.Error("a table array that Marshal would write as sections was accepted as a key/value line")
	}
}

// A mark that raises the limit for a path raises it for the edit as well, so a
// program writing the format its own parser produces is not refused.
func TestEditDocument_WritesANewTableArrayTheParserInlines(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineMaxKeys(4))
	data, err := p.Unmarshal([]byte(noListSrc))
	if err != nil {
		t.Fatal("parse the fixture:", err)
	}
	data["rewrites"] = []any{map[string]any{
		"match": "forge@org/repo", "bucket": "work", "org": "org", "repo": "repo",
	}}

	out, err := kongfig.EditDocument(p, []byte(noListSrc), data)
	if err != nil {
		t.Fatal("edit:", err)
	}
	want := `# where the checkouts live
dirname = "."
rewrites = [{bucket = "work", match = "forge@org/repo", org = "org", repo = "repo"}]

[buckets.all]
match = ["@"]
`
	if string(out) != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", out, want)
	}
}

// An element with a table inside it has no inline form at all — TOML cannot nest
// a table in an inline table — so it needs the section the document does not
// have.
func TestEditDocument_RefusesANewTableArrayWithANestedTable(t *testing.T) {
	_, err := editTOML(t, noListSrc, func(d kongfig.ConfigData) {
		d["rewrites"] = []any{map[string]any{
			"match": "forge@org/repo",
			"into":  map[string]any{"bucket": "work"},
		}}
	})
	if err == nil {
		t.Error("a table array element with a table inside it was accepted as a key/value line")
	}
}

// A table is the other way round: a section is its written form, so a new key
// holding one is still refused rather than collapsed onto a line the author
// never asked for.
func TestEditDocument_RefusesANewTableKey(t *testing.T) {
	_, err := editTOML(t, noListSrc, func(d kongfig.ConfigData) {
		d["forges"] = map[string]any{"github": "github.com"}
	})
	if err == nil {
		t.Error("a new table key was accepted")
	}
}
