package toml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
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
		"tags":   []any{},
		"labels": kongfig.ConfigData{},
		"count":  0,
		"name":   "",
		"kept":   "",
	}

	var buf bytes.Buffer
	ctx := omitEmptyCtx("tags", "labels", "count", "name")
	if err := tomlparser.Default.Bind(plainStyler{}).Render(ctx, &buf, data); err != nil {
		t.Fatal("render:", err)
	}

	out := buf.String()
	for _, gone := range []string{"tags", "labels", "count", "name"} {
		if strings.Contains(out, gone) {
			t.Errorf("marked empty key %q was rendered:\n%s", gone, out)
		}
	}
	if !strings.Contains(out, `kept = ""`) {
		t.Errorf("unmarked zero value was dropped:\n%s", out)
	}
}

// A marked key that holds something is written like any other.
func TestRender_KeepsMarkedKeysThatHoldSomething(t *testing.T) {
	data := kongfig.ConfigData{"tags": []any{"a"}, "count": 1}

	var buf bytes.Buffer
	if err := tomlparser.Default.Bind(plainStyler{}).Render(omitEmptyCtx("tags", "count"), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	for _, want := range []string{`tags = ["a"]`, "count = 1"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q:\n%s", want, buf.String())
		}
	}
}

// The mark also applies inside an inlined table, where the reader cannot delete
// the key by hand afterwards.
func TestRender_OmitsEmptyKeysInsideInlineTables(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("roots.*"))
	data := kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{"path": "/dev", "alias": ""},
		},
	}

	var buf bytes.Buffer
	if err := p.Bind(plainStyler{}).Render(omitEmptyCtx("roots.*.alias"), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	if strings.Contains(buf.String(), "alias") {
		t.Errorf("marked empty key survived inlining:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `path = "/dev"`) {
		t.Errorf("sibling was dropped too:\n%s", buf.String())
	}
}

// Writing a config file honors the marks the same way rendering does, so the
// file and the shown config agree.
func TestMarshalCtx_OmitsEmptyMarkedKeys(t *testing.T) {
	data := kongfig.ConfigData{"tags": []any{}, "kept": ""}

	b, err := tomlparser.Default.MarshalCtx(omitEmptyCtx("tags"), data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if strings.Contains(string(b), "tags") {
		t.Errorf("marked empty key was written:\n%s", b)
	}
	if !strings.Contains(string(b), `kept = ""`) {
		t.Errorf("unmarked zero value was dropped:\n%s", b)
	}
}

// A redacted placeholder stands in for a value that is set, so the mark leaves
// it alone.
func TestRender_OmitEmptyKeepsRedactedPlaceholder(t *testing.T) {
	data := kongfig.ConfigData{
		"token": kongfig.RenderedValue{Redacted: true, RedactedDisplay: "***"},
	}

	var buf bytes.Buffer
	if err := tomlparser.Default.Bind(plainStyler{}).Render(omitEmptyCtx("token"), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	if !strings.Contains(buf.String(), "***") {
		t.Errorf("redacted placeholder dropped as empty:\n%s", buf.String())
	}
}
