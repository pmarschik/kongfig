package json_test

import (
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
)

// The cases in this file are the ones where a key that goes in and a key that
// goes out meet at the same comma, and the ones where the document is not laid
// out the way an editor would like it to be. They are where an in-place editor
// breaks a document if it is going to break one at all.

// Renaming a key is a key added and a key removed at once, and both of them are
// about the comma that held the old key to its neighbor.
func TestEditDocument_RenamesTheLastKey(t *testing.T) {
	got := mustEditJSON(t, "{\n  \"host\": \"localhost\",\n  \"port\": 8080\n}\n", func(d kongfig.ConfigData) {
		delete(d, "port")
		d["listen"] = float64(8080)
	})
	want := `{
  "host": "localhost",
  "listen": 8080
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_RenamesTheOnlyKeyOfASingleLineObject(t *testing.T) {
	got := mustEditJSON(t, `{"host": "localhost"}`+"\n", func(d kongfig.ConfigData) {
		delete(d, "host")
		d["listen"] = "localhost"
	})
	want := `{"listen": "localhost"}` + "\n"
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Every key replaced: nothing of the object stays, so the new keys have no member
// to join and go in right after the brace that opens it.
func TestEditDocument_ReplacesEveryKey(t *testing.T) {
	got := mustEditJSON(t, "{\n  \"host\": \"localhost\",\n  \"port\": 8080\n}\n", func(d kongfig.ConfigData) {
		clear(d)
		d["listen"] = "localhost:8080"
	})
	want := `{
  "listen": "localhost:8080"
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Two keys dropped, and one of them the last: the first takes its line, and the
// comma left over after the key that stays goes as well.
func TestEditDocument_RemovesTheLastTwoKeys(t *testing.T) {
	src := `{
  "host": "localhost",
  "port": 8080,
  "timeout": 30
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		delete(d, "port")
		delete(d, "timeout")
	})
	want := `{
  "host": "localhost"
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A document may put two keys on one line even though the object as a whole is
// written across lines. Taking the line away would take the neighbor with it, so
// the key goes out with its comma instead.
func TestEditDocument_RemovesAKeyThatSharesItsLine(t *testing.T) {
	src := `{
  "host": "localhost", "port": 8080,
  "timeout": 30
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		delete(d, "host")
	})
	want := `{
  "port": 8080,
  "timeout": 30
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditDocument_RemovesTheSecondOfTwoKeysOnOneLine(t *testing.T) {
	src := `{
  "host": "localhost", "port": 8080,
  "timeout": 30
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		delete(d, "port")
	})
	want := `{
  "host": "localhost",
  "timeout": 30
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// The last key of an object may share its line with the key before it. It has no
// comma of its own to go away with, so the comma in front of it goes, and the
// space that comma held goes with it.
func TestEditDocument_RemovesTheLastKeyOfASharedLine(t *testing.T) {
	src := `{
  "timeout": 30,
  "host": "localhost", "port": 8080
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		delete(d, "port")
	})
	want := `{
  "timeout": 30,
  "host": "localhost"
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Every element replaced: the array keeps its brackets and its layout, and the
// elements that go in do not have to wait for the ones going out.
func TestEditDocument_ReplacesEveryArrayElement(t *testing.T) {
	src := `{
  "archive": [
    "*.log",
    "*.tmp"
  ]
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.bak"}
	})
	want := `{
  "archive": [
    "*.bak"
  ]
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An element added at the front and the one at the back taken away: the new
// element goes in above the element that stays, and the comma the last element
// left behind goes too.
func TestEditDocument_AddsAtTheFrontAndRemovesAtTheBack(t *testing.T) {
	src := `{
  "archive": [
    "*.log",
    "*.tmp"
  ]
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.bak", "*.log"}
	})
	want := `{
  "archive": [
    "*.bak",
    "*.log"
  ]
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A compact document stays compact: a new key joins it without the spaces a
// document written for a reader would carry.
func TestEditDocument_KeepsACompactDocumentCompact(t *testing.T) {
	out, err := editJSON(t, jsonparser.Compact, `{"host":"localhost"}`, func(d kongfig.ConfigData) {
		d["port"] = float64(8080)
		d["db"] = kongfig.ConfigData{"name": "mydb"}
	})
	if err != nil {
		t.Fatal("edit:", err)
	}
	want := `{"host":"localhost","db":{"name":"mydb"},"port":8080}`
	if out != want {
		t.Errorf("edited document:\n got: %s\nwant: %s", out, want)
	}
}

// A key the document writes with an escape is the key the data has, so an edit of
// its value finds it.
func TestEditDocument_FindsAKeyWrittenWithAnEscape(t *testing.T) {
	src := `{
  "a.b": "one"
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		d["a.b"] = "two"
	})
	want := strings.Replace(src, `"one"`, `"two"`, 1)
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An array written on one line inside a document written across lines takes its
// new element on that line.
func TestEditDocument_AppendsToAnArrayOnOneLine(t *testing.T) {
	src := `{
  "archive": ["*.log"]
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		d["archive"] = []any{"*.log", "*.tmp"}
	})
	want := `{
  "archive": ["*.log", "*.tmp"]
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// An object nested in an array element keeps the indentation of the element it
// belongs to when it is written in whole.
func TestEditDocument_AddsAnObjectToAnArray(t *testing.T) {
	src := `{
  "dbs": [
    {
      "name": "one"
    }
  ]
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		d["dbs"] = append(asList(t, d["dbs"]), kongfig.ConfigData{"name": "two"})
	})
	want := `{
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
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A blank line is layout, not data, so it stays where its author put it.
func TestEditDocument_KeepsBlankLines(t *testing.T) {
	src := `{
  "host": "localhost",

  "port": 8080
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		d["port"] = float64(9090)
	})
	want := strings.Replace(src, "8080", "9090", 1)
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A key added to an object nested two levels down sits at the indentation of that
// object, not of the document.
func TestEditDocument_AddsAKeyTwoLevelsDown(t *testing.T) {
	src := `{
  "db": {
    "primary": {
      "host": "localhost"
    }
  }
}
`
	got := mustEditJSON(t, src, func(d kongfig.ConfigData) {
		asObject(t, asObject(t, d["db"])["primary"])["port"] = float64(5432)
	})
	want := `{
  "db": {
    "primary": {
      "host": "localhost",
      "port": 5432
    }
  }
}
`
	if got != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A comment block above a key belongs to that key: the key goes away with it, and
// the key that stays keeps its own.
func TestEditDocument_KeepsCommentsWithTheirKeys(t *testing.T) {
	src := `{
  // where it lives
  "host": "localhost",
  // how long to wait
  "timeout": 30
}
`
	out, err := editJSON(t, jsonparser.WithComments, src, func(d kongfig.ConfigData) {
		delete(d, "timeout")
	})
	if err != nil {
		t.Fatal("edit:", err)
	}
	want := `{
  // where it lives
  "host": "localhost"
}
`
	if out != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", out, want)
	}
}
