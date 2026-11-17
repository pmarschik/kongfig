package show

import (
	"bytes"
	"os"
	"testing"
)

func TestColorDisabled(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm")
	if colorDisabled(&bytes.Buffer{}, "yaml") {
		t.Error("buffer + yaml should keep colors")
	}
	if !colorDisabled(&bytes.Buffer{}, "json") {
		t.Error("json must never be colorized")
	}
	t.Setenv("NO_COLOR", "1")
	if !colorDisabled(&bytes.Buffer{}, "yaml") {
		t.Error("NO_COLOR must disable colors")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if !colorDisabled(&bytes.Buffer{}, "yaml") {
		t.Error("TERM=dumb must disable colors")
	}
	t.Setenv("TERM", "xterm")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if !colorDisabled(w, "yaml") {
		t.Error("pipe must disable colors")
	}
}
