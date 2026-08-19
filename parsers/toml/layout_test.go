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

// The width gate still ends in a section header, once the reflow that normally
// answers an over-wide table is off.
func TestRender_InlineTable_TooWideForTerminal_StaysBlock(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"), tomlparser.WithInlineWrap(false))
	ctx := render.WithTTYSizeCtx(context.Background(), 20, 0)
	out := renderPlain(ctx, t, p, data)

	want := "  [buckets.work]\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected block table on a narrow terminal\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_InlineTable_OverflowMark_StaysInline(t *testing.T) {
	// The mark says the one-line form is worth more than a line that ends inside
	// the window, so the width check no longer demotes it.
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"), tomlparser.WithInlineOverflow("buckets.*"))
	ctx := render.WithTTYSizeCtx(context.Background(), 20, 0)
	out := renderPlain(ctx, t, p, data)

	want := "  work = {path = \"/a/very/long/path/that/does/not/fit\"}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the marked table to keep its line\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_InlineTable_OverflowMarkAlone_Inlines(t *testing.T) {
	// An overflow mark implies an inline one: asking for the compact form past the
	// edge of the window is asking for the compact form.
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	p := tomlparser.New(tomlparser.WithInlineOverflow("buckets.*"))
	out := renderPlain(context.Background(), t, p, data)

	want := "  work = {path = \"/w\"}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the overflow mark to inline on its own\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_InlineTable_OverflowFromContext_StaysInline(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	ctx := kongfig.InlineOverflowKey.WithCtx(context.Background(), map[string]bool{"buckets.*": true})
	ctx = render.WithTTYSizeCtx(ctx, 20, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	want := "  work = {path = \"/a/very/long/path/that/does/not/fit\"}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the schema-marked path to keep its line\nwant substring:\n%q\ngot:\n%s", want, out)
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

// rulesParser marks the rules array, which for an array of tables only moves the
// key limit — the way a ,inline=N struct tag on the field would.
func rulesParser(opts ...tomlparser.Option) *tomlparser.Parser {
	return tomlparser.New(append([]tomlparser.Option{tomlparser.WithInlineTables("rules")}, opts...)...)
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
	// An entry that wraps onto as many lines as its block form would take has
	// nothing left to win, so the block form — the more explicit of the two —
	// keeps it. Two keys wrapping onto three lines in a 30-column terminal is
	// exactly that tie.
	data := rewriteData(kongfig.ConfigData{"match": "@/a-very-long-pattern-that-does-not-fit-anywhere", "org": "uploaded"})

	ctx := render.WithTTYSizeCtx(context.Background(), 30, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if !strings.Contains(out, "[[rules]]") {
		t.Errorf("expected section headers for an oversized entry:\n%s", out)
	}
}

func TestRender_TableArray_OversizedEntryWithOverflowMark_GoesOnePerLine(t *testing.T) {
	// Same tie as the test above, with the mark added: the shape of the list is
	// what the reader is after, so it keeps a line per rule instead of a header
	// per rule even where wrapping buys nothing.
	data := rewriteData(kongfig.ConfigData{"match": "@/a-very-long-pattern-that-does-not-fit-anywhere", "org": "uploaded"})

	p := tomlparser.New(tomlparser.WithInlineOverflow("rules"))
	ctx := render.WithTTYSizeCtx(context.Background(), 30, 0)
	out := renderPlain(ctx, t, p, data)

	if strings.Contains(out, "[[rules]]") {
		t.Errorf("the overflow mark did not hold off the section headers:\n%s", out)
	}
	// How wide an entry is allowed to run before its own keys wrap is the
	// emitter's business; what the mark decides is that the entry stays an entry.
	want := "rules = [\n  {match = \"@/a-very-long-pattern-that-does-not-fit-anywhere\","
	if !strings.Contains(out, want) {
		t.Errorf("expected one entry per line\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_TableArray_OverflowFromContext_GoesOnePerLine(t *testing.T) {
	data := rewriteData(kongfig.ConfigData{"match": "@/a-very-long-pattern-that-does-not-fit-anywhere", "org": "uploaded"})

	ctx := kongfig.InlineOverflowKey.WithCtx(context.Background(), map[string]bool{"rules": true})
	ctx = render.WithTTYSizeCtx(ctx, 30, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if strings.Contains(out, "[[rules]]") {
		t.Errorf("the schema mark did not hold off the section headers:\n%s", out)
	}
}

func TestRender_TableArray_EntryBarelyOverTheWidth_WrapsItsPairs(t *testing.T) {
	// An entry a few columns too wide is the case the width test used to get
	// wrong: one overflowing entry demoted the whole array, trading a single
	// wrapped line for a header plus a line per key. Wrapping wins until it
	// costs as much as the block form — and the entry breaks between its pairs,
	// indented under the first one, rather than being left to the terminal, which
	// would break mid-token at column zero.
	data := rewriteData(
		kongfig.ConfigData{"match": "a", "org": "b"},
		kongfig.ConfigData{"match": "@/a-pattern-just-over-the-line", "org": "uploaded"},
	)

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if strings.Contains(out, "[[rules]]") {
		t.Errorf("one entry over the width demoted the whole array:\n%s", out)
	}
	want := "  {match = \"a\", org = \"b\"},\n" +
		"  {match = \"@/a-pattern-just-over-the-line\",\n" +
		"   org = \"uploaded\"},\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the wide entry wrapped between its pairs\nwant substring:\n%q\ngot:\n%s", want, out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if render.VisualWidth(line) > 60 {
			t.Errorf("line runs past the terminal (%d cols): %q", render.VisualWidth(line), line)
		}
	}
}

func TestRender_TableArray_WrappedEntry_ReparsesUnchanged(t *testing.T) {
	// The wrap relies on TOML 1.1 allowing a newline inside an inline table, so
	// pin that the wrapped document still reads back as the same data.
	data := rewriteData(
		kongfig.ConfigData{"match": "@/a-pattern-just-over-the-line", "org": "uploaded"},
	)

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.New(tomlparser.WithInlineOverflow("rules")), data)
	if !strings.Contains(out, ",\n   org") {
		t.Fatalf("expected a wrapped entry to reparse; nothing wrapped:\n%s", out)
	}

	back, err := tomlparser.Default.Unmarshal([]byte(out))
	if err != nil {
		t.Fatalf("wrapped inline table did not reparse: %v\n%s", err, out)
	}
	rules, ok := back["rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("rules did not survive the round trip: %#v\n%s", back["rules"], out)
	}
	elem, isTable := rules[0].(kongfig.ConfigData)
	if !isTable {
		t.Fatalf("entry = %T, want ConfigData", rules[0])
	}
	if elem["match"] != "@/a-pattern-just-over-the-line" || elem["org"] != "uploaded" {
		t.Errorf("reparsed entry = %#v, want the original", elem)
	}
}

func TestRender_TableArray_EntryOverMaxKeys_StaysBlock(t *testing.T) {
	// The entries are no longer small objects, so the array gets sections even
	// though it fits the terminal. One entry past the limit settles all of them.
	data := rewriteData(
		kongfig.ConfigData{"match": "a", "org": "b"},
		kongfig.ConfigData{"match": "c", "org": "d", "bucket": "e", "vcs": "git"},
	)

	ctx := render.WithTTYSizeCtx(context.Background(), 200, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if !strings.Contains(out, "[[rules]]") {
		t.Errorf("expected section headers past the key limit:\n%s", out)
	}
}

func TestRender_TableArray_WithInlineMaxKeys_RaisesTheLimit(t *testing.T) {
	data := rewriteData(kongfig.ConfigData{"match": "c", "org": "d", "bucket": "e", "vcs": "git"})

	ctx := render.WithTTYSizeCtx(context.Background(), 200, 0)
	out := renderPlain(ctx, t, rulesParser(tomlparser.WithInlineMaxKeys(4)), data)

	want := "rules = [{bucket = \"e\", match = \"c\", org = \"d\", vcs = \"git\"}]\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected an inline array at the raised limit\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_TableArray_FromContext_HonorsPerPathMaxKeys(t *testing.T) {
	// The schema route carries its own limit per marked path, so a field tagged
	// ,inline=4 inlines entries the default limit would have blocked.
	data := rewriteData(kongfig.ConfigData{"match": "c", "org": "d", "bucket": "e", "vcs": "git"})

	ctx := kongfig.InlineTablesKey.WithCtx(context.Background(), map[string]int{"rules": 4})
	ctx = render.WithTTYSizeCtx(ctx, 200, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	want := "rules = [{bucket = \"e\", match = \"c\", org = \"d\", vcs = \"git\"}]\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the schema mark to inline the array\nwant substring:\n%q\ngot:\n%s", want, out)
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
