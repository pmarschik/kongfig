package kongfig_test

import (
	"errors"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// layered builds the situation an editing program is actually in: a file on
// disk, and layers above it that never belonged to the file.
func layered(t *testing.T) *kongfig.Kongfig {
	t.Helper()
	k := kongfig.New()
	if err := k.LoadParsed(kongfig.ConfigData{"host": "filehost", "port": "8080"}, "file", kongfig.WithParser(lineParser{})); err != nil {
		t.Fatal(err)
	}
	if err := k.LoadParsed(kongfig.ConfigData{"host": "envhost"}, "env.tag"); err != nil {
		t.Fatal(err)
	}
	return k
}

const layerDoc = `# where it lives
host = filehost
port = 8080
`

// The data an edit writes back is the layer's own, not the merged view. Passing
// the merge would write the env value into the file and drop every key the merge
// dropped, which is the mistake this entry point exists to prevent.
func TestEditLayer_WritesBackTheLayersOwnData(t *testing.T) {
	out, err := layered(t).EditLayer("file", []byte(layerDoc), func(d kongfig.ConfigData) error {
		d["port"] = "9090"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `# where it lives
host = filehost
port = 9090
`
	if string(out) != want {
		t.Errorf("edited document:\n got:\n%s\nwant:\n%s", out, want)
	}
}

// The edit runs on a copy: a program that changes its mind, or fails halfway,
// leaves the loaded configuration as it was.
func TestEditLayer_LeavesTheLoadedDataAlone(t *testing.T) {
	k := layered(t)
	if _, err := k.EditLayer("file", []byte(layerDoc), func(d kongfig.ConfigData) error {
		d["port"] = "9090"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := k.All()["port"]; got != "8080" {
		t.Errorf("loaded port = %v, want it unchanged at 8080", got)
	}
}

// A caller that gives up mid-edit hears its own error back, and gets no bytes to
// write.
func TestEditLayer_ReturnsTheErrorFromTheEdit(t *testing.T) {
	giveUp := errors.New("give up")
	out, err := layered(t).EditLayer("file", []byte(layerDoc), func(kongfig.ConfigData) error {
		return giveUp
	})
	if !errors.Is(err, giveUp) {
		t.Errorf("err = %v, want the error the edit returned", err)
	}
	if out != nil {
		t.Errorf("out = %q, want no document", out)
	}
}

// A source label no layer carries is a mistake in the caller, and it says so
// rather than editing some other layer.
func TestEditLayer_RefusesAnUnknownSource(t *testing.T) {
	_, err := layered(t).EditLayer("nowhere", []byte(layerDoc), func(kongfig.ConfigData) error {
		return nil
	})
	if !errors.Is(err, kongfig.ErrNoSuchLayer) {
		t.Errorf("err = %v, want ErrNoSuchLayer", err)
	}
}

// An env, flag or default layer has no document behind it. There is nothing to
// edit in place, and the caller hears that instead of a document it never read.
func TestEditLayer_RefusesALayerWithoutADocument(t *testing.T) {
	_, err := layered(t).EditLayer("env.tag", []byte(layerDoc), func(kongfig.ConfigData) error {
		return nil
	})
	if !errors.Is(err, kongfig.ErrLayerHasNoDocument) {
		t.Errorf("err = %v, want ErrLayerHasNoDocument", err)
	}
}

// The edit goes through kongfig.EditDocument, so a rewrite the parser cannot
// verify is still an error rather than a file the caller writes.
func TestEditLayer_VerifiesTheRewrite(t *testing.T) {
	_, err := layered(t).EditLayer("file", []byte(layerDoc), func(d kongfig.ConfigData) error {
		d["added"] = "a key the document has no line for"
		return nil
	})
	if err == nil {
		t.Error("an edit the document cannot express was accepted")
	}
}
