package toml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	render "github.com/pmarschik/kongfig/render"
)

// renderPlain renders data with p and an unstyled Styler and returns the output.
func renderPlain(ctx context.Context, t *testing.T, p *tomlparser.Parser, data kongfig.ConfigData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := p.Bind(plainStyler{}).Render(ctx, &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	return buf.String()
}

func TestRender_WithIndentEmpty_DisablesIndentation(t *testing.T) {
	data := kongfig.ConfigData{
		"server": kongfig.ConfigData{
			"tls": kongfig.ConfigData{"enabled": true},
		},
	}

	p := tomlparser.New(tomlparser.WithIndent(""))
	out := renderPlain(context.Background(), t, p, data)

	want := "[server]\n[server.tls]\nenabled = true\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected flush-left output\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_WithIndentTab_UsesConfiguredString(t *testing.T) {
	data := kongfig.ConfigData{"server": kongfig.ConfigData{"port": int64(8080)}}

	p := tomlparser.New(tomlparser.WithIndent("\t"))
	out := renderPlain(context.Background(), t, p, data)

	want := "[server]\n\tport = 8080\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected tab indentation\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// bucketData is a two-level map whose second level is the inlining candidate.
func bucketData(entry kongfig.ConfigData) kongfig.ConfigData {
	return kongfig.ConfigData{"buckets": kongfig.ConfigData{"work": entry}}
}

func TestRender_InlineTablePattern_UnderMaxKeys_RendersInline(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w", "color": "blue"})

	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"))
	out := renderPlain(context.Background(), t, p, data)

	want := "[buckets]\n  work = {color = \"blue\", path = \"/w\"}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected inline table\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_InlineTablePattern_OverMaxKeys_StaysBlock(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"a": int64(1), "b": int64(2), "c": int64(3), "d": int64(4)})

	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"))
	out := renderPlain(context.Background(), t, p, data)

	want := "  [buckets.work]\n    a = 1\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected block table past the key limit\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_WithInlineMaxKeys_RaisesTheLimit(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"a": int64(1), "b": int64(2), "c": int64(3), "d": int64(4)})

	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"), tomlparser.WithInlineMaxKeys(4))
	out := renderPlain(context.Background(), t, p, data)

	want := "  work = {a = 1, b = 2, c = 3, d = 4}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected inline table at the raised limit\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_InlineTable_TooWideForTerminal_StaysBlock(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"))
	ctx := render.WithTTYSizeCtx(context.Background(), 20, 0)
	out := renderPlain(ctx, t, p, data)

	want := "  [buckets.work]\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected block table on a narrow terminal\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestMarshal_InlineTable_IgnoresTerminalWidth(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"))
	b, err := p.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	want := "  work = {path = \"/a/very/long/path/that/does/not/fit\"}\n"
	if !strings.Contains(string(b), want) {
		t.Errorf("expected width-independent inline table on write\nwant substring:\n%q\ngot:\n%s", want, b)
	}
}

func TestRender_InlineTablesFromContext_RendersInline(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	ctx := kongfig.InlineTablesKey.WithCtx(context.Background(), map[string]int{"buckets.*": 0})
	out := renderPlain(ctx, t, tomlparser.Default, data)

	want := "  work = {path = \"/w\"}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected schema-marked path to inline\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_InlineTablesFromContext_HonorsPerPathMaxKeys(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"a": int64(1), "b": int64(2)})

	ctx := kongfig.InlineTablesKey.WithCtx(context.Background(), map[string]int{"buckets.*": 1})
	out := renderPlain(ctx, t, tomlparser.Default, data)

	want := "  [buckets.work]\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the per-path limit to force a block table\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_NestedTables_IndentedByDepth(t *testing.T) {
	data := kongfig.ConfigData{
		"server": kongfig.ConfigData{
			"tls": kongfig.ConfigData{"enabled": true},
		},
	}

	out := renderPlain(context.Background(), t, tomlparser.Default, data)

	want := "[server]\n  [server.tls]\n    enabled = true\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected depth-indented nesting\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}
