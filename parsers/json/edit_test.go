package json_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
)

// editJSON is the flow a program that changes a config file goes through: read
// the document, parse it, edit the data, write the bytes that come back. The
// rewrite runs through kongfig.EditDocument, so every case here also asserts that
// the result parses back to the data it asked for.
func editJSON(t *testing.T, p kongfig.Parser, src string, edit func(kongfig.ConfigData)) (string, error) {
	t.Helper()
	data, err := p.Unmarshal([]byte(src))
	if err != nil {
		t.Fatal("parse the fixture:", err)
	}
	edit(data)
	out, err := kongfig.EditDocument(p, []byte(src), data)
	return string(out), err
}

// mustEditJSON fails the test when the edit is refused.
func mustEditJSON(t *testing.T, src string, edit func(kongfig.ConfigData)) string {
	t.Helper()
	out, err := editJSON(t, jsonparser.Default, src, edit)
	if err != nil {
		t.Fatal("edit:", err)
	}
	return out
}

const objectSrc = `{
  "host": "localhost",
  "port": 8080,
  "db": {
    "name": "mydb"
  }
}
`

// The point of editing in place: everything the author wrote that the data change
// does not touch is still there, byte for byte. Marshal would sort the keys and
// respell the number.
func TestEditDocument_RewritesOneValue(t *testing.T) {
	got := mustEditJSON(t, objectSrc, func(d kongfig.ConfigData) {
		d["port"] = float64(9090)
	})
	want := `{
  "host": "localhost",
  "port": 9090,
  "db": {
    "name": "mydb"
  }
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_RewritesANestedValue(t *testing.T) {
	got := mustEditJSON(t, objectSrc, func(d kongfig.ConfigData) {
		asObject(t, d["db"])["name"] = "otherdb"
	})
	want := strings.Replace(objectSrc, `"mydb"`, `"otherdb"`, 1)
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A value the data does not change keeps its text, so a number the author wrote
// as 8080.0 or 1e3 is not respelled behind their back.
func TestEditDocument_LeavesUnchangedValuesAlone(t *testing.T) {
	src := `{
  "port": 8080.0,
  "ratio": 1e3,
  "host": "localhost"
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		d["host"] = "otherhost"
	})
	want := strings.Replace(src, `"localhost"`, `"otherhost"`, 1)
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A new key joins the layout of the object it goes into: on a line of its own,
// with the indentation of the keys already there, after the last of them.
func TestEditDocument_AddsAKeyToAMultiLineObject(t *testing.T) {
	got := mustEditJSON(t, objectSrc, func(d kongfig.ConfigData) {
		d["timeout"] = float64(30)
	})
	want := `{
  "host": "localhost",
  "port": 8080,
  "db": {
    "name": "mydb"
  },
  "timeout": 30
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_AddsAKeyToANestedObject(t *testing.T) {
	got := mustEditJSON(t, objectSrc, func(d kongfig.ConfigData) {
		asObject(t, d["db"])["port"] = float64(5432)
	})
	want := `{
  "host": "localhost",
  "port": 8080,
  "db": {
    "name": "mydb",
    "port": 5432
  }
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An object written on one line takes a new key on that line, not on a new one:
// the layout the author chose is the layout the edit writes in.
func TestEditDocument_AddsAKeyToASingleLineObject(t *testing.T) {
	got := mustEditJSON(t, `{"host": "localhost"}`+"\n", func(d kongfig.ConfigData) {
		d["port"] = float64(8080)
	})
	want := `{"host": "localhost", "port": 8080}` + "\n"
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_AddsTheFirstKeyOfAnEmptyObject(t *testing.T) {
	got := mustEditJSON(t, "{}\n", func(d kongfig.ConfigData) {
		d["host"] = "localhost"
	})
	want := `{
  "host": "localhost"
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A key the data drops takes its line with it, and the comma that held it to its
// neighbor goes too — a document with a dangling comma is not a document.
func TestEditDocument_RemovesAKey(t *testing.T) {
	got := mustEditJSON(t, objectSrc, func(d kongfig.ConfigData) {
		delete(d, "port")
	})
	want := `{
  "host": "localhost",
  "db": {
    "name": "mydb"
  }
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_RemovesTheLastKey(t *testing.T) {
	got := mustEditJSON(t, objectSrc, func(d kongfig.ConfigData) {
		delete(d, "db")
	})
	want := `{
  "host": "localhost",
  "port": 8080
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_RemovesTheOnlyKey(t *testing.T) {
	got := mustEditJSON(t, "{\n  \"host\": \"localhost\"\n}\n", func(d kongfig.ConfigData) {
		delete(d, "host")
	})
	want := "{\n}\n"
	if got != want {
		t.Errorf("edited document:\n got:\n%q\nwant:\n%q", got, want)
	}
}

// A key whose value is a whole object is written out with the indentation of the
// line it lands on, so it reads like the rest of the document.
func TestEditDocument_AddsAKeyHoldingAnObject(t *testing.T) {
	got := mustEditJSON(t, "{\n  \"host\": \"localhost\"\n}\n", func(d kongfig.ConfigData) {
		d["db"] = kongfig.ConfigData{"name": "mydb"}
	})
	want := `{
  "host": "localhost",
  "db": {
    "name": "mydb"
  }
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

const arraySrc = `{
  "archive": [
    "*.log",
    "*.tmp"
  ]
}
`

func TestEditDocument_AppendsAnArrayElement(t *testing.T) {
	got := mustEditJSON(t, arraySrc, func(d kongfig.ConfigData) {
		d["archive"] = append(asList(t, d["archive"]), "*.bak")
	})
	want := `{
  "archive": [
    "*.log",
    "*.tmp",
    "*.bak"
  ]
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_RemovesAnArrayElement(t *testing.T) {
	got := mustEditJSON(t, arraySrc, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.tmp"}
	})
	want := `{
  "archive": [
    "*.tmp"
  ]
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_AppendsToASingleLineArray(t *testing.T) {
	got := mustEditJSON(t, `{"archive": ["*.log"]}`+"\n", func(d kongfig.ConfigData) {
		d["archive"] = append(asList(t, d["archive"]), "*.tmp")
	})
	want := `{"archive": ["*.log", "*.tmp"]}` + "\n"
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_AppendsToAnEmptyArray(t *testing.T) {
	got := mustEditJSON(t, `{"archive": []}`+"\n", func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.log"}
	})
	want := `{"archive": ["*.log"]}` + "\n"
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An element added in the middle goes in where the data puts it, and the elements
// around it keep the text they had.
func TestEditDocument_InsertsAnArrayElementInTheMiddle(t *testing.T) {
	got := mustEditJSON(t, arraySrc, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.log", "*.bak", "*.tmp"}
	})
	want := `{
  "archive": [
    "*.log",
    "*.bak",
    "*.tmp"
  ]
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An element that holds an object is edited in place like any other value, so the
// elements beside it are not rewritten.
func TestEditDocument_EditsAnObjectInsideAnArray(t *testing.T) {
	src := `{
  "dbs": [
    {
      "name": "one"
    },
    {
      "name": "two"
    }
  ]
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		asObject(t, asList(t, d["dbs"])[0])["name"] = "uno"
	})
	want := strings.Replace(src, `"one"`, `"uno"`, 1)
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A JSONC document keeps its comments: the editor works on the bytes the author
// wrote, and a comment is text no data change is about.
func TestEditDocument_KeepsJSONCComments(t *testing.T) {
	src := `{
  // where it lives
  "host": "localhost",
  "port": 8080 // the port
}
`
	got, err := editJSON(t, jsonparser.WithComments, src, func(d kongfig.ConfigData) {
		d["host"] = "otherhost"
	})
	if err != nil {
		t.Fatal("edit:", err)
	}
	want := strings.Replace(src, `"localhost"`, `"otherhost"`, 1)
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A comment written above an element is about that element, so an element that
// goes away takes it along and a new one goes in above it.
func TestEditDocument_KeepsCommentsWithTheirArrayElements(t *testing.T) {
	src := `{
  "archive": [
    // logs pile up
    "*.log",
    "*.tmp" // short-lived
  ]
}
`
	got, err := editJSON(t, jsonparser.WithComments, src, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.tmp", "*.bak"}
	})
	if err != nil {
		t.Fatal("edit:", err)
	}
	want := `{
  "archive": [
    "*.tmp", // short-lived
    "*.bak"
  ]
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A document whose root is not an object holds no keys to edit, so there is
// nothing an edit could mean.
func TestEditDocument_RefusesANonObjectRoot(t *testing.T) {
	out, err := jsonparser.Default.EditDocument([]byte("[1, 2]\n"), kongfig.ConfigData{})
	if err == nil {
		t.Fatal("a document with no object in it was accepted")
	}
	if out != nil {
		t.Errorf("out = %q, want no document", out)
	}
}

// A value JSON has no text for is refused, and the document is left alone rather
// than written half over.
func TestEditDocument_RefusesAValueJSONCannotWrite(t *testing.T) {
	_, err := editJSON(t, jsonparser.Default, objectSrc, func(d kongfig.ConfigData) {
		d["ratio"] = math.NaN()
	})
	if !errors.Is(err, jsonparser.ErrCannotEdit) {
		t.Errorf("err = %v, want ErrCannotEdit", err)
	}
}

// A parse error is reported as one, rather than being written over.
func TestEditDocument_RefusesADocumentItCannotParse(t *testing.T) {
	_, err := kongfig.EditDocument(jsonparser.Default, []byte("{oops}\n"), kongfig.ConfigData{})
	if err == nil {
		t.Error("a broken document was accepted")
	}
}

// The whole flow in one call, the way a program that maintains a config file
// uses it.
func TestApply_EditsAJSONDocument(t *testing.T) {
	src := `{
  "archive": [
    "*.log"
  ],
  "name": "yard"
}
`
	out, err := kongfig.Apply(jsonparser.Default, []byte(src),
		kongfig.Append("archive", "*.bak"),
		kongfig.Set("name", "yard.example"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "archive": [
    "*.log",
    "*.bak"
  ],
  "name": "yard.example"
}
`
	if string(out) != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", out, want)
	}
}

// asObject views a parsed value as an object so a test can change what is inside
// it.
func asObject(t *testing.T, v any) kongfig.ConfigData {
	t.Helper()
	obj, ok := v.(kongfig.ConfigData)
	if !ok {
		t.Fatalf("the parse gave %T where an object was expected", v)
	}
	return obj
}

// asList views a parsed value as a list so a test can append to it.
func asList(t *testing.T, v any) []any {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("the parse gave %T where a list was expected", v)
	}
	return list
}
