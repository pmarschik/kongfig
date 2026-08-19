package toml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	"github.com/pmarschik/kongfig/render"
)

func sourced(v any, layer string) kongfig.RenderedValue {
	return kongfig.RenderedValue{
		Value:  v,
		Source: kongfig.SourceMeta{Layer: kongfig.LayerMeta{Kind: layer, Name: layer}},
	}
}

func renderInline(t *testing.T, data kongfig.ConfigData) string {
	t.Helper()

	p := tomlparser.New(tomlparser.WithInlineTables("roots.*"))
	var buf bytes.Buffer
	if err := p.Bind(plainStyler{}).Render(context.Background(), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	return buf.String()
}

// Collapsing a table into an inline table must not throw away where its leaves
// came from: a subtree whose leaves all share one source keeps that source as a
// single annotation on the line.
func TestRender_InlineTableKeepsSingleSourceAnnotation(t *testing.T) {
	out := renderInline(t, kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{
				"path": sourced("/dev", "workdir"),
				"name": sourced("dev", "workdir"),
			},
		},
	})

	if !strings.Contains(out, "developer = {") {
		t.Fatalf("table was not inlined:\n%s", out)
	}
	if !strings.Contains(out, "# workdir") {
		t.Errorf("inlined table lost its source annotation:\n%s", out)
	}
}

// When the leaves disagree, one label would be a lie, so each key is named.
func TestRender_InlineTableKeepsPerKeyAnnotations(t *testing.T) {
	out := renderInline(t, kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{
				"path": sourced("/dev", "workdir"),
				"name": sourced("dev", "xdg"),
			},
		},
	})

	if !strings.Contains(out, "developer = {") {
		t.Fatalf("table was not inlined:\n%s", out)
	}
	for _, want := range []string{"name: xdg", "path: workdir"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing per-key annotation %q:\n%s", want, out)
		}
	}
}

// Unsourced leaves carry no annotation, so the line gets none either.
func TestRender_InlineTableWithoutSourcesHasNoAnnotation(t *testing.T) {
	out := renderInline(t, kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{"path": "/dev"},
		},
	})

	if strings.Contains(out, "#") {
		t.Errorf("unsourced inline table gained an annotation:\n%s", out)
	}
}

// --no-comments and Marshal suppress annotations, inlined or not.
func TestRender_InlineTableAnnotationsSuppressedWithoutComments(t *testing.T) {
	data := kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{
				"path": sourced("/dev", "workdir"),
				"name": sourced("dev", "xdg"),
			},
		},
	}

	p := tomlparser.New(tomlparser.WithInlineTables("roots.*"))
	var buf bytes.Buffer
	ctx := kongfig.RenderNoCommentsKey.WithCtx(context.Background(), true)
	if err := p.Bind(plainStyler{}).Render(ctx, &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	if strings.Contains(buf.String(), "#") {
		t.Errorf("annotation written with comments off:\n%s", buf.String())
	}

	b, err := p.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if strings.Contains(string(b), "#") {
		t.Errorf("annotation written by Marshal:\n%s", b)
	}
}

// Nested keys are named by their path within the inlined table.
func TestRender_InlineTableAnnotatesNestedKeysByPath(t *testing.T) {
	out := renderInline(t, kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{
				"path": sourced("/dev", "workdir"),
				"tls":  kongfig.ConfigData{"key": sourced("k", "xdg")},
			},
		},
	})

	if !strings.Contains(out, "tls.key: xdg") {
		t.Errorf("nested key not named by its path:\n%s", out)
	}
}

// Naming every key separately repeats the same label once per key. Keys that
// share a source are named together, so the label is written once.
func TestRender_InlineTableGroupsKeysBySharedSource(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("roots.*"), tomlparser.WithInlineMaxKeys(4))
	var buf bytes.Buffer
	err := p.Bind(plainStyler{}).Render(context.Background(), &buf, kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{
				"hosts":    sourced("h", "file"),
				"priority": sourced(4, "file"),
				"name":     sourced("dev", "derived"),
				"vcs":      sourced("git", "derived"),
			},
		},
	})
	if err != nil {
		t.Fatal("render:", err)
	}
	out := buf.String()

	if !strings.Contains(out, "developer = {") {
		t.Fatalf("table was not inlined:\n%s", out)
	}
	if !strings.Contains(out, "hosts, priority: file") {
		t.Errorf("keys sharing a source were not grouped:\n%s", out)
	}
	if !strings.Contains(out, "name, vcs: derived") {
		t.Errorf("keys sharing a source were not grouped:\n%s", out)
	}
}

// An annotation that would push the line past the terminal width goes above the
// entry instead of wrapping mid-line, one group per line.
func TestRender_InlineTableAnnotationWrapsOntoOwnLines(t *testing.T) {
	data := kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{
				"a": sourced(1, "file (/Users/someone/.config/yard/config.yaml)"),
				"b": sourced(2, "derived"),
			},
		},
	}

	p := tomlparser.New(tomlparser.WithInlineTables("roots.*"))
	var buf bytes.Buffer
	ctx := render.WithTTYSizeCtx(context.Background(), 60, 24)
	if err := p.Bind(plainStyler{}).Render(ctx, &buf, data); err != nil {
		t.Fatal("render:", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	var entry int
	for i, l := range lines {
		if strings.Contains(l, "developer = {") {
			entry = i
		}
	}
	if entry == 0 {
		t.Fatalf("table was not inlined:\n%s", buf.String())
	}
	if strings.Contains(lines[entry], "#") {
		t.Errorf("annotation stayed on the entry line:\n%s", buf.String())
	}
	for _, l := range lines {
		if render.VisualWidth(l) > 60 {
			t.Errorf("line exceeds terminal width:\n%s", buf.String())
		}
	}
	if !strings.Contains(lines[entry-1], "b: derived") || !strings.Contains(lines[entry-2], "a: file") {
		t.Errorf("annotation groups not written one per line above the entry:\n%s", buf.String())
	}
}

// Arrays of tables written as key/value lines lose the same information.
func TestRender_InlineTableArrayKeepsAnnotations(t *testing.T) {
	var buf bytes.Buffer
	data := kongfig.ConfigData{
		"rules": []any{kongfig.ConfigData{"match": sourced("a", "workdir")}},
	}
	if err := tomlparser.Default.Bind(plainStyler{}).Render(context.Background(), &buf, data); err != nil {
		t.Fatal("render:", err)
	}

	if !strings.Contains(buf.String(), "# workdir") {
		t.Errorf("inlined table array lost its source annotation:\n%s", buf.String())
	}
}
