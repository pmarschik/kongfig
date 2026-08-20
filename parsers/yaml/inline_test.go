package yaml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	render "github.com/pmarschik/kongfig/render"
)

// bucketData is a map of named entries, each small enough to read on one line —
// the shape the ,inline mark exists for.
func bucketData(sub kongfig.ConfigData) kongfig.ConfigData {
	return kongfig.ConfigData{"buckets": kongfig.ConfigData{"work": sub}}
}

func renderYAML(ctx context.Context, t *testing.T, p *yamlparser.Parser, data kongfig.ConfigData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := p.Bind(plainStyler{}).Render(ctx, &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	return buf.String()
}

func sourcedValue(v any, name string) kongfig.RenderedValue {
	return kongfig.RenderedValue{
		Value:  v,
		Source: kongfig.SourceMeta{Layer: kongfig.LayerMeta{Kind: kongfig.KindDefaults, Name: name}},
	}
}

// A marked mapping collapses into YAML's flow form, which is the same compact
// shape parsers/toml writes as an inline table.
func TestRender_InlineMapping_MarkedPathRendersFlow(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"))
	out := renderYAML(context.Background(), t, p, data)

	want := "  work: {path: /w}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected a flow mapping\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// Nothing changes for a mapping nobody marked.
func TestRender_InlineMapping_UnmarkedStaysBlock(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	out := renderYAML(context.Background(), t, yamlparser.Default, data)

	want := "  work:\n    path: /w\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected a block mapping\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// The compact form is a display choice, and one that no longer fits the window
// is no longer compact: the mapping falls back to the block form.
func TestRender_InlineMapping_TooWideForTheTerminal_FallsBackToBlock(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"))
	ctx := render.WithTTYSizeCtx(context.Background(), 20, 0)
	out := renderYAML(ctx, t, p, data)

	want := "  work:\n    path: /a/very/long/path/that/does/not/fit\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the block form on a narrow terminal\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// An overflow mark says the one-line shape is worth a line that runs past the
// edge of the window, so the width check no longer demotes it.
func TestRender_InlineMapping_OverflowMark_StaysInline(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"), yamlparser.WithInlineOverflow("buckets.*"))
	ctx := render.WithTTYSizeCtx(context.Background(), 20, 0)
	out := renderYAML(ctx, t, p, data)

	want := "  work: {path: /a/very/long/path/that/does/not/fit}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the marked mapping to keep its line\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// An overflow mark implies an inline one, exactly as it does for TOML.
func TestRender_InlineMapping_OverflowMarkAlone_Inlines(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	p := yamlparser.New(yamlparser.WithInlineOverflow("buckets.*"))
	out := renderYAML(context.Background(), t, p, data)

	want := "  work: {path: /w}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the overflow mark to inline on its own\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// The marks a ,inline struct tag publishes reach the renderer through the
// context, so the tag drives YAML with no parser option at all.
func TestRender_InlineMappingFromContext_RendersFlow(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	ctx := kongfig.InlineTablesKey.WithCtx(context.Background(), map[string]int{"buckets.*": 0})
	out := renderYAML(ctx, t, yamlparser.Default, data)

	want := "  work: {path: /w}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the schema-marked path to inline\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// A ,inline=N tag carries its own key limit; a mapping over it keeps the block
// form however well it would fit.
func TestRender_InlineMappingFromContext_HonorsPerPathMaxKeys(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"a": 1, "b": 2})

	ctx := kongfig.InlineTablesKey.WithCtx(context.Background(), map[string]int{"buckets.*": 1})
	out := renderYAML(ctx, t, yamlparser.Default, data)

	want := "  work:\n"
	if !strings.Contains(out, want) || strings.Contains(out, "{") {
		t.Errorf("expected the per-path limit to force a block mapping\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// Past the default limit a marked mapping is no longer compact enough to read on
// one line.
func TestRender_InlineMapping_OverTheDefaultKeyLimit_StaysBlock(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"a": 1, "b": 2, "c": 3, "d": 4})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"))
	out := renderYAML(context.Background(), t, p, data)

	if strings.Contains(out, "{") {
		t.Errorf("expected a block mapping past the default key limit:\n%s", out)
	}
}

// A written file must not depend on the width of the terminal that produced it.
func TestMarshal_InlineMapping_IgnoresTerminalWidth(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"))
	b, err := p.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	want := "work: {path: /a/very/long/path/that/does/not/fit}\n"
	if !strings.Contains(string(b), want) {
		t.Errorf("expected a width-independent flow mapping on write\nwant substring:\n%q\ngot:\n%s", want, b)
	}
}

// The write path reads the same context marks the render path does.
func TestMarshalCtx_InlineMappingFromContext_WritesFlow(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	ctx := kongfig.InlineTablesKey.WithCtx(context.Background(), map[string]int{"buckets.*": 0})
	b, err := yamlparser.Default.MarshalCtx(ctx, data)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	if !strings.Contains(string(b), "work: {path: /w}\n") {
		t.Errorf("expected a flow mapping on write:\n%s", b)
	}
}

// A document written in its own key order keeps that order inside the mappings
// that collapsed: the two marks describe different things and both apply.
func TestMarshalCtx_InlineMapping_KeepsKeyOrder(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w", "quota": 10})

	ctx := kongfig.WithRenderKeyOrderCtx(context.Background(),
		map[string][]string{"buckets.work": {"quota", "path"}})
	ctx = kongfig.InlineTablesKey.WithCtx(ctx, map[string]int{"buckets.*": 0})
	b, err := yamlparser.Default.MarshalCtx(ctx, data)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	if !strings.Contains(string(b), "work: {quota: 10, path: /w}\n") {
		t.Errorf("expected the document order inside the flow mapping:\n%s", b)
	}
}

// Marking nothing leaves the encoder's own output alone, byte for byte.
func TestMarshal_WithoutMarks_IsUnchanged(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	want, err := yamlparser.Default.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	got, err := yamlparser.New().Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("configured parser changed unmarked output\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// The compact form is still YAML: it has to come back as the data it was built
// from, both from a rendered document and from a written one.
func TestInlineMapping_Reparses(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w", "quota": 10})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"))
	rendered := renderYAML(context.Background(), t, p, data)
	written, err := p.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	for name, doc := range map[string]string{"render": rendered, "marshal": string(written)} {
		t.Run(name, func(t *testing.T) {
			back, err := yamlparser.Default.Unmarshal([]byte(doc))
			if err != nil {
				t.Fatalf("did not reparse: %v\n%s", err, doc)
			}
			work, ok := back.LookupPath("buckets.work")
			if !ok {
				t.Fatalf("buckets.work is gone:\n%s", doc)
			}
			sub, ok := work.(kongfig.ConfigData)
			if !ok {
				t.Fatalf("buckets.work is %T, want a mapping:\n%s", work, doc)
			}
			if sub["path"] != "/w" || sub["quota"] != 10 {
				t.Errorf("reparsed %#v, want the original\n%s", sub, doc)
			}
		})
	}
}

// A value the caller marked as hidden stays hidden in the compact form too.
func TestRender_InlineMapping_KeepsRedactionInside(t *testing.T) {
	data := bucketData(kongfig.ConfigData{
		"token": kongfig.RenderedValue{Value: "s3cret", Redacted: true, RedactedDisplay: "***"},
	})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"))
	out := renderYAML(context.Background(), t, p, data)

	if strings.Contains(out, "s3cret") {
		t.Errorf("the collapsed mapping leaked a redacted value:\n%s", out)
	}
	if !strings.Contains(out, "  work: {token: ***}\n") {
		t.Errorf("expected the placeholder inside the flow mapping:\n%s", out)
	}
}

// Collapsing the mapping collapses the lines its values would have annotated, so
// the provenance moves onto the line that replaced them.
func TestRender_InlineMapping_KeepsProvenance(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": sourcedValue("/w", "defaults")})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"))
	out := renderYAML(context.Background(), t, p, data)

	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if !strings.Contains(line, "{path: /w}") {
			continue
		}
		if !strings.Contains(line, "#") || !strings.Contains(line, "defaults") {
			t.Errorf("the collapsed mapping lost its provenance:\n%s", out)
		}
		return
	}
	t.Errorf("no flow mapping in the output:\n%s", out)
}

// Keys left out by an omitempty mark are keys the line does not have to hold, so
// they do not count against the limit either.
func TestRender_InlineMapping_OmitEmptyKeysDoNotCount(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w", "alias": "", "note": ""})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"), yamlparser.WithInlineMaxKeys(1))
	out := renderYAML(omitEmptyCtx("buckets.*.alias", "buckets.*.note"), t, p, data)

	want := "  work: {path: /w}\n"
	if !strings.Contains(out, want) {
		t.Errorf("expected the dropped keys to be left out of the count\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

// Block collections are a global "spell everything out", so they outrank a mark
// that asks for the compact shape.
func TestRender_InlineMapping_BlockCollectionsWins(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/w"})

	p := yamlparser.New(yamlparser.WithInlineMaps("buckets.*"))
	out := renderYAML(kongfig.WithRenderBlockCollectionsCtx(context.Background()), t, p, data)

	if strings.Contains(out, "{") {
		t.Errorf("expected the block form under WithRenderBlockCollections:\n%s", out)
	}
}
