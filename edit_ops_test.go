package kongfig_test

import (
	"errors"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// applied runs the edits over a copy of d and hands back what they left behind,
// so each test reads as the data before and the data after.
func applied(t *testing.T, d kongfig.ConfigData, edits ...kongfig.Edit) kongfig.ConfigData {
	t.Helper()
	out := d.Clone()
	if err := kongfig.ApplyEdits(out, edits...); err != nil {
		t.Fatal(err)
	}
	return out
}

func editError(t *testing.T, d kongfig.ConfigData, edits ...kongfig.Edit) error {
	t.Helper()
	err := kongfig.ApplyEdits(d.Clone(), edits...)
	if err == nil {
		t.Fatal("the edit was accepted, want an error")
	}
	return err
}

func TestSet_ReplacesAScalar(t *testing.T) {
	got := applied(t, kongfig.ConfigData{"host": "a"}, kongfig.Set("host", "b"))
	if got["host"] != "b" {
		t.Errorf("host = %v, want b", got["host"])
	}
}

// A path that goes deeper than the data does is a path the edit builds on its
// way down: setting one value must not need a separate call per level.
func TestSet_BuildsTheMappingsOnTheWayDown(t *testing.T) {
	got := applied(t, kongfig.ConfigData{}, kongfig.Set("db.primary.host", "a"))
	if v, ok := got.LookupPath("db.primary.host"); !ok || v != "a" {
		t.Errorf("db.primary.host = %v (found %t), want a", v, ok)
	}
}

// A map handed to an edit is normalized the way [kongfig.ToConfigData]
// normalizes a parse, so what a later edit walks into is a ConfigData.
func TestSet_NormalizesAMapItStores(t *testing.T) {
	got := applied(t, kongfig.ConfigData{}, kongfig.Set("db", map[string]any{"host": "a"}))
	if _, ok := got["db"].(kongfig.ConfigData); !ok {
		t.Fatalf("db is %T, want kongfig.ConfigData", got["db"])
	}
}

// Bracket indexing is how the rest of the library addresses an element of a
// list, and an edit addresses one the same way.
func TestSet_ReplacesAListElement(t *testing.T) {
	got := applied(t,
		kongfig.ConfigData{"hosts": []any{"a", "b", "c"}},
		kongfig.Set("hosts[1]", "b.example"))
	want := []any{"a", "b.example", "c"}
	if !kongfig.EqualValues(got["hosts"], want) {
		t.Errorf("hosts = %v, want %v", got["hosts"], want)
	}
}

func TestSet_ReachesThroughAListElement(t *testing.T) {
	got := applied(t,
		kongfig.ConfigData{"dbs": []any{kongfig.ConfigData{"host": "a"}}},
		kongfig.Set("dbs[0].host", "b"))
	if v, _ := got.LookupPath("dbs"); !kongfig.EqualValues(v, []any{kongfig.ConfigData{"host": "b"}}) {
		t.Errorf("dbs = %v, want the host of the first element changed to b", v)
	}
}

// Setting a path through a scalar would have to throw that scalar away. The
// edit says so instead.
func TestSet_RefusesToReachThroughAScalar(t *testing.T) {
	err := editError(t, kongfig.ConfigData{"host": "a"}, kongfig.Set("host.port", 1))
	if !errors.Is(err, kongfig.ErrPathNotMapping) {
		t.Errorf("err = %v, want ErrPathNotMapping", err)
	}
}

func TestUnset_RemovesTheKey(t *testing.T) {
	got := applied(t, kongfig.ConfigData{"host": "a", "port": 8080}, kongfig.Unset("port"))
	if _, present := got["port"]; present {
		t.Errorf("port is still there: %v", got)
	}
}

// An edit that removes nothing is a mistake in the caller: the path it names is
// not the path the document has. Silence would hide a typo until the file was
// written.
func TestUnset_ReportsAPathThatIsNotThere(t *testing.T) {
	err := editError(t, kongfig.ConfigData{"host": "a"}, kongfig.Unset("timeout"))
	if !errors.Is(err, kongfig.ErrNoSuchPath) {
		t.Errorf("err = %v, want ErrNoSuchPath", err)
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("err = %v, want it to name the path", err)
	}
}

func TestAppend_AddsToTheEndOfTheList(t *testing.T) {
	got := applied(t,
		kongfig.ConfigData{"archive": []any{"*.log"}},
		kongfig.Append("archive", "*.bak", "*.tmp"))
	want := []any{"*.log", "*.bak", "*.tmp"}
	if !kongfig.EqualValues(got["archive"], want) {
		t.Errorf("archive = %v, want %v", got["archive"], want)
	}
}

func TestAppend_ReportsAValueThatIsNotAList(t *testing.T) {
	err := editError(t, kongfig.ConfigData{"host": "a"}, kongfig.Append("host", "b"))
	if !errors.Is(err, kongfig.ErrPathNotList) {
		t.Errorf("err = %v, want ErrPathNotList", err)
	}
}

func TestRemoveAt_TakesTheElementOut(t *testing.T) {
	got := applied(t,
		kongfig.ConfigData{"hosts": []any{"a", "b", "c"}},
		kongfig.RemoveAt("hosts", 1))
	want := []any{"a", "c"}
	if !kongfig.EqualValues(got["hosts"], want) {
		t.Errorf("hosts = %v, want %v", got["hosts"], want)
	}
}

func TestRemoveAt_ReportsAnIndexPastTheEnd(t *testing.T) {
	err := editError(t, kongfig.ConfigData{"hosts": []any{"a"}}, kongfig.RemoveAt("hosts", 3))
	if !errors.Is(err, kongfig.ErrNoSuchPath) {
		t.Errorf("err = %v, want ErrNoSuchPath", err)
	}
}

// The edits run in the order they are given, over the same data, so a later one
// sees what an earlier one did.
func TestApplyEdits_RunsTheEditsInOrder(t *testing.T) {
	got := applied(t, kongfig.ConfigData{"hosts": []any{"a"}},
		kongfig.Append("hosts", "b"),
		kongfig.RemoveAt("hosts", 0))
	if !kongfig.EqualValues(got["hosts"], []any{"b"}) {
		t.Errorf("hosts = %v, want [b]", got["hosts"])
	}
}

// Apply is the whole round trip: parse the document, fold the edits into the
// data, rewrite the text that changed, and check the result.
func TestApply_RewritesTheDocument(t *testing.T) {
	src := []byte("# where it lives\nhost = a\nport = 8080\n")
	out, err := kongfig.Apply(lineParser{}, src, kongfig.Set("port", "9090"))
	if err != nil {
		t.Fatal(err)
	}
	want := "# where it lives\nhost = a\nport = 9090\n"
	if string(out) != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", out, want)
	}
}

// An edit that the data refuses stops there: no document comes back, and the
// parser is never asked to rewrite anything.
func TestApply_StopsAtAnEditTheDataRefuses(t *testing.T) {
	src := []byte("host = a\n")
	out, err := kongfig.Apply(lineParser{}, src, kongfig.Unset("timeout"))
	if !errors.Is(err, kongfig.ErrNoSuchPath) {
		t.Errorf("err = %v, want ErrNoSuchPath", err)
	}
	if out != nil {
		t.Errorf("out = %q, want no document", out)
	}
}

// A parser without an in-place editor reports that through Apply as well, so a
// caller can fall back to Marshal.
func TestApply_ReportsAParserWithNoEditor(t *testing.T) {
	_, err := kongfig.Apply(uneditableParser{}, []byte("host = a\n"), kongfig.Set("host", "b"))
	if !errors.Is(err, kongfig.ErrNoDocumentEditor) {
		t.Errorf("err = %v, want ErrNoDocumentEditor", err)
	}
}
