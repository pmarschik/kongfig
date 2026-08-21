package yaml_test

import (
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
)

// kongfig.Apply is the whole flow in one call: parse the document, fold the
// edits into the data, rewrite the text that changed. This is the case a program
// that maintains a config file actually has — a list to add to, another to take
// from, and comments that must stay where their author put them.
func TestApply_EditsTheListsAndKeepsTheComments(t *testing.T) {
	src := `# what to archive
archive:
  # logs pile up
  - "*.log"
  - "*.tmp" # short-lived
name: yard
`
	out, err := kongfig.Apply(yamlparser.Default, []byte(src),
		kongfig.Append("archive", "*.bak"),
		kongfig.RemoveAt("archive", 0),
		kongfig.Set("name", "yard.example"))
	if err != nil {
		t.Fatal(err)
	}
	want := `# what to archive
archive:
  - "*.tmp" # short-lived
  - "*.bak"
name: yard.example
`
	if string(out) != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", out, want)
	}
}

// A path that the document does not have stops the edit before the parser is
// asked for anything, so the document is never rewritten from a typo.
func TestApply_StopsAtAPathTheDocumentDoesNotHave(t *testing.T) {
	out, err := kongfig.Apply(yamlparser.Default, []byte("name: yard\n"),
		kongfig.Unset("timeout"))
	if err == nil {
		t.Fatal("the edit was accepted, want an error")
	}
	if out != nil {
		t.Errorf("out = %q, want no document", out)
	}
}
