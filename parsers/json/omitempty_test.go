package json_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
)

func omitEmptyCtx(paths ...string) context.Context {
	marks := make(map[string]bool, len(paths))
	for _, p := range paths {
		marks[p] = true
	}
	return kongfig.OmitEmptyKey.WithCtx(context.Background(), marks)
}

// A key marked omitempty is left out while it holds nothing; the zero value of
// an unmarked key is still the configuration and stays.
func TestRender_OmitsEmptyMarkedKeys(t *testing.T) {
	data := kongfig.ConfigData{
		"tags":  []any{},
		"count": 0,
		"name":  "",
		"kept":  "",
	}

	var buf bytes.Buffer
	ctx := omitEmptyCtx("tags", "count", "name")
	if err := jsonparser.Default.Bind(plainStyler{}).Render(ctx, &buf, data); err != nil {
		t.Fatal("render:", err)
	}

	out := buf.String()
	for _, gone := range []string{"tags", "count", "name"} {
		if strings.Contains(out, gone) {
			t.Errorf("marked empty key %q was rendered:\n%s", gone, out)
		}
	}
	if !strings.Contains(out, `"kept"`) {
		t.Errorf("unmarked zero value was dropped:\n%s", out)
	}
	// Dropping the last key must not leave the comma that separated it.
	if !json.Valid([]byte(out)) {
		t.Errorf("output is not valid JSON:\n%s", out)
	}
}

// An object left with nothing to show takes its own key with it, comma
// included, rather than leaving a dangling separator behind.
func TestRender_OmitsObjectsEmptiedByTheMark(t *testing.T) {
	data := kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{"path": "/dev", "alias": ""},
			"scratch":   kongfig.ConfigData{"alias": ""},
		},
	}

	var buf bytes.Buffer
	if err := jsonparser.Default.Bind(plainStyler{}).Render(omitEmptyCtx("roots.*.alias"), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	out := buf.String()
	if strings.Contains(out, "alias") {
		t.Errorf("marked empty key survived nesting:\n%s", out)
	}
	if strings.Contains(out, "scratch") {
		t.Errorf("object left with nothing kept its key:\n%s", out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("output is not valid JSON:\n%s", out)
	}
}

// A marked key that holds something is written like any other.
func TestRender_KeepsMarkedKeysThatHoldSomething(t *testing.T) {
	data := kongfig.ConfigData{"tags": []any{"a"}, "count": 1}

	var buf bytes.Buffer
	if err := jsonparser.Default.Bind(plainStyler{}).Render(omitEmptyCtx("tags", "count"), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	for _, want := range []string{`"tags"`, `"count": 1`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q:\n%s", want, buf.String())
		}
	}
}

// A redacted placeholder stands in for a value that is set, so the mark leaves
// it alone.
func TestRender_OmitEmptyKeepsRedactedPlaceholder(t *testing.T) {
	data := kongfig.ConfigData{
		"token": kongfig.RenderedValue{Redacted: true, RedactedDisplay: "***"},
	}

	var buf bytes.Buffer
	if err := jsonparser.Default.Bind(plainStyler{}).Render(omitEmptyCtx("token"), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	if !strings.Contains(buf.String(), "***") {
		t.Errorf("redacted placeholder dropped as empty:\n%s", buf.String())
	}
}
