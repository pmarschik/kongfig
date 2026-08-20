package kongfig_test

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// lineParser is a whole format in a dozen lines: one "key = value" pair per
// line, string values, a "#" comment line kept verbatim. Small enough to read,
// real enough that the round-trip check has something to parse.
type lineParser struct{}

func (lineParser) Unmarshal(src []byte) (kongfig.ConfigData, error) {
	out := kongfig.ConfigData{}
	for line := range strings.SplitSeq(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			return nil, fmt.Errorf("line %q has no value", line)
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out, nil
}

func (lineParser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	var b strings.Builder
	for _, key := range slices.Sorted(maps.Keys(data)) {
		fmt.Fprintf(&b, "%s = %v\n", key, data[key])
	}
	return []byte(b.String()), nil
}

// EditDocument rewrites the value of each key want mentions, drops the line of a
// key it does not, and leaves comments and key order where they are. A key with
// no line in the document is an edit this format cannot express.
func (lineParser) EditDocument(src []byte, want kongfig.ConfigData) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	kept := make([]string, 0, len(lines))
	edited := map[string]bool{}
	for _, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			kept = append(kept, line)
			continue
		}
		name := strings.TrimSpace(key)
		value, wanted := want[name]
		if !wanted {
			continue // a key want does not mention is a key that goes away
		}
		kept = append(kept, fmt.Sprintf("%s = %v", name, value))
		edited[name] = true
	}
	for name := range want {
		if !edited[name] {
			return nil, fmt.Errorf("no line for %q", name)
		}
	}
	return []byte(strings.Join(kept, "\n")), nil
}

// uneditableParser is the same format without an editor, for the fallback path.
type uneditableParser struct{}

func (uneditableParser) Unmarshal(src []byte) (kongfig.ConfigData, error) {
	return lineParser{}.Unmarshal(src)
}

func (uneditableParser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	return lineParser{}.Marshal(data)
}

// sloppyEditor writes something other than what it was asked for — the mistake
// the parse-back check exists to catch.
type sloppyEditor struct{ lineParser }

func (sloppyEditor) EditDocument(src []byte, _ kongfig.ConfigData) ([]byte, error) {
	return append(slices.Clone(src), []byte("sneaked = in\n")...), nil
}

// brokenEditor produces bytes its own parser cannot read.
type brokenEditor struct{ lineParser }

func (brokenEditor) EditDocument([]byte, kongfig.ConfigData) ([]byte, error) {
	return []byte("this line has no value"), nil
}

// refusingEditor stands for an edit the format cannot express.
type refusingEditor struct{ lineParser }

var errCannotExpress = errors.New("cannot express that as an edit")

func (refusingEditor) EditDocument([]byte, kongfig.ConfigData) ([]byte, error) {
	return nil, errCannotExpress
}

const editSrc = "# the port the server listens on\nport = 8080\nhost = localhost\n"

// The point of the whole exercise: the caller edits data, and the bytes that come
// back are the document it started from with one value changed — comment, key
// order and spacing untouched.
func TestEditDocument_RewritesInPlace(t *testing.T) {
	out, err := kongfig.EditDocument(lineParser{}, []byte(editSrc),
		kongfig.ConfigData{"port": "9090", "host": "localhost"})
	if err != nil {
		t.Fatal("edit:", err)
	}
	want := "# the port the server listens on\nport = 9090\nhost = localhost\n"
	if string(out) != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", out, want)
	}
}

// A parser with no editor is not an error in the data — the caller may have no
// layout to protect and can fall back to Marshal, so it has to be able to tell
// this apart from a refused edit.
func TestEditDocument_WithoutEditor(t *testing.T) {
	out, err := kongfig.EditDocument(uneditableParser{}, []byte(editSrc),
		kongfig.ConfigData{"port": "9090"})
	if !errors.Is(err, kongfig.ErrNoDocumentEditor) {
		t.Errorf("err = %v, want ErrNoDocumentEditor", err)
	}
	if out != nil {
		t.Errorf("bytes returned alongside the error: %q", out)
	}
}

// An editor that cannot express the change says so, and its own error survives
// so the caller can tell why.
func TestEditDocument_RefusedEditKeepsTheReason(t *testing.T) {
	if _, err := kongfig.EditDocument(refusingEditor{}, []byte(editSrc),
		kongfig.ConfigData{"port": "9090"}); !errors.Is(err, errCannotExpress) {
		t.Errorf("err = %v, want the editor's own error", err)
	}
	if _, err := kongfig.EditDocument(lineParser{}, []byte(editSrc),
		kongfig.ConfigData{"missing": "x"}); err == nil {
		t.Error("editing a key the document has no line for was accepted")
	}
}

// Text surgery is only as safe as its verification: bytes that no longer hold
// what was asked for never reach the caller, who would otherwise write them.
func TestEditDocument_VerifiesTheResult(t *testing.T) {
	out, err := kongfig.EditDocument(sloppyEditor{}, []byte(editSrc),
		kongfig.ConfigData{"port": "9090"})
	if err == nil {
		t.Fatalf("a rewrite that changed the wrong thing was accepted:\n%s", out)
	}
	if out != nil {
		t.Errorf("bytes returned alongside the error: %q", out)
	}
	if errors.Is(err, kongfig.ErrNoDocumentEditor) {
		t.Error("a botched rewrite reported as a missing editor")
	}
}

// A rewrite whose result does not parse is caught by the same check, rather than
// leaving the caller to discover it the next time the file is read.
func TestEditDocument_RejectsUnparsableResult(t *testing.T) {
	if _, err := kongfig.EditDocument(brokenEditor{}, []byte(editSrc),
		kongfig.ConfigData{"port": "9090"}); err == nil {
		t.Error("a rewrite that does not parse was accepted")
	}
}

// What the caller passes is the whole document, not a patch: a key it left out
// is a key that goes away, and the surrounding lines still stay as they were.
func TestEditDocument_WantIsTheWholeDocument(t *testing.T) {
	out, err := kongfig.EditDocument(lineParser{}, []byte(editSrc),
		kongfig.ConfigData{"port": "8080"})
	if err != nil {
		t.Fatal("edit:", err)
	}
	want := "# the port the server listens on\nport = 8080\n"
	if string(out) != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", out, want)
	}
}
