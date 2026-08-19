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

// rewriteData is an array of small tables, the shape a list of rules has.
func rewriteData(entries ...kongfig.ConfigData) kongfig.ConfigData {
	elems := make([]any, len(entries))
	for i, e := range entries {
		elems[i] = e
	}
	return kongfig.ConfigData{"rules": elems}
}

func TestRender_TableArray_FitsOnOneLine_StaysOneLine(t *testing.T) {
	data := rewriteData(kongfig.ConfigData{"match": "a", "org": "b"})

	ctx := render.WithTTYSizeCtx(context.Background(), 80, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	want := "rules = [{match = \"a\", org = \"b\"}]\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected a one-line table array\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_TableArray_TooWideForOneLine_GoesOnePerLine(t *testing.T) {
	// The entries are small; only the list of them is long. Each keeps its own
	// line rather than each earning a [[rules]] header.
	data := rewriteData(
		kongfig.ConfigData{"match": "@/cluster-config*", "org": "uploaded"},
		kongfig.ConfigData{"match": "@/upl-*", "org": "uploaded"},
		kongfig.ConfigData{"match": "@mikazuki", "org": "pmarschik"},
	)

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	want := "rules = [\n" +
		"  {match = \"@/cluster-config*\", org = \"uploaded\"},\n" +
		"  {match = \"@/upl-*\", org = \"uploaded\"},\n" +
		"  {match = \"@mikazuki\", org = \"pmarschik\"},\n" +
		"]\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected one entry per line\nwant substring:\n%q\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "[[rules]]") {
		t.Errorf("small entries were given section headers:\n%s", out)
	}
}

func TestRender_TableArray_EntryTooWideForALine_StaysBlock(t *testing.T) {
	// One entry that does not fit a line of its own has nowhere left to go: the
	// per-line form would overflow just as the one-line form did.
	data := rewriteData(kongfig.ConfigData{"match": "@/a-very-long-pattern-that-does-not-fit-anywhere", "org": "uploaded"})

	ctx := render.WithTTYSizeCtx(context.Background(), 30, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if !strings.Contains(out, "[[rules]]") {
		t.Errorf("expected section headers for an oversized entry:\n%s", out)
	}
}

func TestRender_TableArray_NestedTable_StaysBlock(t *testing.T) {
	// Inline TOML cannot express a nested table, so this one has no other form.
	data := rewriteData(kongfig.ConfigData{"match": "a", "opts": kongfig.ConfigData{"deep": true}})

	ctx := render.WithTTYSizeCtx(context.Background(), 200, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if !strings.Contains(out, "[[rules]]") {
		t.Errorf("expected section headers for a nested table:\n%s", out)
	}
}

func TestRender_TableArray_PerLine_ReparsesUnchanged(t *testing.T) {
	entries := []kongfig.ConfigData{
		{"match": "@/cluster-config*", "org": "uploaded"},
		{"match": "@/upl-*", "org": "uploaded"},
	}
	data := rewriteData(entries...)

	ctx := render.WithTTYSizeCtx(context.Background(), 50, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	back, err := tomlparser.Default.Unmarshal([]byte(out))
	if err != nil {
		t.Fatalf("the per-line form does not parse: %v\n%s", err, out)
	}
	got, ok := back["rules"].([]any)
	if !ok || len(got) != len(entries) {
		t.Fatalf("rules did not survive the round trip: %#v\n%s", back["rules"], out)
	}
	for i, want := range entries {
		elem, isTable := got[i].(kongfig.ConfigData)
		if !isTable {
			t.Errorf("entry %d = %T, want ConfigData", i, got[i])
			continue
		}
		if elem["match"] != want["match"] || elem["org"] != want["org"] {
			t.Errorf("entry %d = %#v, want %#v", i, elem, want)
		}
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
