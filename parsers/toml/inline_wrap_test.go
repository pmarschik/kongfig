package toml_test

import (
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	"github.com/pmarschik/kongfig/render"
)

// fieldsData is the shape a map of small config objects has: one entry holding a
// nested object, which is what a section per entry buries.
func fieldsData() kongfig.ConfigData {
	return kongfig.ConfigData{"fields": kongfig.ConfigData{"blocked_by": blockedByEntry()}}
}

// blockedByEntry is one such object: three scalars and a nested object.
func blockedByEntry() kongfig.ConfigData {
	return kongfig.ConfigData{
		"jira":     "",
		"readonly": false,
		"type":     "issue_links",
		"link":     kongfig.ConfigData{"direction": "inward", "type": "Blocks"},
	}
}

func fieldsParser() *tomlparser.Parser {
	return tomlparser.New(tomlparser.WithInlineTables("fields.*"), tomlparser.WithInlineMaxKeys(5))
}

// A table too wide for its line keeps the shape the inline mark asked for and
// reflows: the brace opens the key's line, a pair follows per line, and the
// closing brace lines up under the key.
func TestRender_InlineTable_TooWide_ReflowsOnePairPerLine(t *testing.T) {
	ctx := render.WithTTYSizeCtx(context.Background(), 100, 0)
	out := renderPlain(ctx, t, fieldsParser(), fieldsData())

	want := "\n" + `[fields]
  blocked_by = {
    jira = "",
    link = {direction = "inward", type = "Blocks"},
    readonly = false,
    type = "issue_links"
  }
`
	if out != want {
		t.Errorf("reflow:\n%s\nwant:\n%s", out, want)
	}
}

// The reflow relies on TOML 1.1 allowing a newline inside an inline table, so pin
// that the document still reads back as the same data.
func TestRender_InlineTable_ReflowedTable_ReparsesUnchanged(t *testing.T) {
	ctx := render.WithTTYSizeCtx(context.Background(), 100, 0)
	out := renderPlain(ctx, t, fieldsParser(), fieldsData())

	back, err := tomlparser.Default.Unmarshal([]byte(out))
	if err != nil {
		t.Fatalf("reflowed inline table did not reparse: %v\n%s", err, out)
	}
	fields, ok := back["fields"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("fields = %T, want ConfigData\n%s", back["fields"], out)
	}
	entry, isTable := fields["blocked_by"].(kongfig.ConfigData)
	if !isTable {
		t.Fatalf("blocked_by = %T, want ConfigData\n%s", fields["blocked_by"], out)
	}
	if entry["type"] != "issue_links" || entry["readonly"] != false {
		t.Errorf("reparsed entry = %#v, want the original", entry)
	}
	link, isLink := entry["link"].(kongfig.ConfigData)
	if !isLink || link["direction"] != "inward" || link["type"] != "Blocks" {
		t.Errorf("nested table did not survive the reflow: %#v", entry["link"])
	}
}

// bucketMatchData is a table whose own line fits once reflowed but whose array
// value does not.
func bucketMatchData() kongfig.ConfigData {
	return kongfig.ConfigData{"buckets": kongfig.ConfigData{
		"stormtrooper": kongfig.ConfigData{
			"dirname": ".",
			"match": []any{
				"github@ixo-corp",
				"github@ixopay-org/*-agent*",
				"github@ixopay-org/eshop*",
				"github@ixopay-org/ixopay-ai-*",
			},
			"nest": true,
			"vcs":  "jj-colocate",
		},
	}}
}

// Reflowing the pairs is not enough when one value is itself over the width. The
// value expands in place, the way it would on a line of its own.
func TestRender_InlineTable_OverWideArrayInside_Expands(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"), tomlparser.WithInlineMaxKeys(5))
	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, p, bucketMatchData())

	want := "\n" + `[buckets]
  stormtrooper = {
    dirname = ".",
    match = [
      "github@ixo-corp",
      "github@ixopay-org/*-agent*",
      "github@ixopay-org/eshop*",
      "github@ixopay-org/ixopay-ai-*",
    ],
    nest = true,
    vcs = "jj-colocate"
  }
`
	if out != want {
		t.Errorf("expanded value:\n%s\nwant:\n%s", out, want)
	}
}

// A nested table that fits its line is left alone: expanding a value that fits
// would spend lines to say the same thing.
func TestRender_InlineTable_NestedTableThatFits_StaysOnOneLine(t *testing.T) {
	ctx := render.WithTTYSizeCtx(context.Background(), 100, 0)
	out := renderPlain(ctx, t, fieldsParser(), fieldsData())

	if !strings.Contains(out, `    link = {direction = "inward", type = "Blocks"},`+"\n") {
		t.Errorf("nested table was expanded although it fits:\n%s", out)
	}
}

// A single pair reflows too. The mark says the table is an entry rather than a
// section, and that reading holds however few pairs it has.
func TestRender_InlineTable_SinglePair_ReflowsToo(t *testing.T) {
	data := bucketData(kongfig.ConfigData{"path": "/a/very/long/path/that/does/not/fit"})

	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"))
	ctx := render.WithTTYSizeCtx(context.Background(), 20, 0)
	out := renderPlain(ctx, t, p, data)

	if strings.Contains(out, "[buckets.work]") {
		t.Errorf("a marked table was demoted instead of reflowed:\n%s", out)
	}
	if !strings.Contains(out, "work = {\n") {
		t.Errorf("expected the reflowed shape:\n%s", out)
	}
}

// The provenance rides the opening brace, the way an expanded array's rides its
// opening bracket: the comment belongs to the key, and no line of the table can
// come between the two.
func TestRender_InlineTable_ReflowedTable_AnnotationRidesTheOpeningBrace(t *testing.T) {
	data := kongfig.ConfigData{
		"fields": kongfig.ConfigData{
			"blocked_by": sourced(blockedByEntry(), "file"),
		},
	}

	ctx := render.WithTTYSizeCtx(context.Background(), 100, 0)
	out := renderPlain(ctx, t, fieldsParser(), data)

	if !strings.Contains(out, "blocked_by = {") {
		t.Fatalf("expected the reflowed shape:\n%s", out)
	}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if strings.Contains(line, "# file") && !strings.Contains(line, "blocked_by = {") {
			t.Errorf("annotation is not on the opening line:\n%s", out)
		}
	}
}

// An annotation that does not fit goes above the whole entry. Moved above its own
// line, as the aligner does for a single line, it would land between the pairs —
// inside the table it annotates.
func TestRender_InlineTable_ReflowedTable_AnnotationNeverLandsBetweenPairs(t *testing.T) {
	// A long source on the reflowed entry, a short one on a sibling that fits: the
	// sibling sets the aligned comment column, and the long annotation does not
	// fit at it, which is when the aligner moves a comment above its line.
	data := kongfig.ConfigData{
		"fields": kongfig.ConfigData{
			"blocked_by": sourced(blockedByEntry(), "file (/etc/storysmith/fields.toml)"),
			"summary":    sourced(kongfig.ConfigData{"jira": "summary", "type": "string"}, "defaults"),
		},
	}

	ctx := render.WithTTYSizeCtx(context.Background(), 70, 0)
	out := renderPlain(ctx, t, fieldsParser(), data)

	// A table is open until its braces balance; a comment written while one is
	// open is a comment between the pairs of that table.
	open := 0
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		delta := strings.Count(line, "{") - strings.Count(line, "}")
		// A comment on the line that closes the table follows the brace, so it is
		// outside it; one on a line that leaves the table open is inside.
		if open > 0 && open+delta > 0 && strings.Contains(line, "#") {
			t.Fatalf("annotation landed inside the table, on line %d:\n%s", i, out)
		}
		open += delta
	}
}

// A newline inside an inline table is TOML 1.1; a 1.0 reader needs the whole
// table on one line, and the section header is the shape that gives it that.
func TestRender_InlineTable_WrapDisabled_DemotesInstead(t *testing.T) {
	p := tomlparser.New(
		tomlparser.WithInlineTables("fields.*"),
		tomlparser.WithInlineMaxKeys(5),
		tomlparser.WithInlineWrap(false),
	)
	ctx := render.WithTTYSizeCtx(context.Background(), 100, 0)
	out := renderPlain(ctx, t, p, fieldsData())

	if !strings.Contains(out, "[fields.blocked_by]") {
		t.Errorf("expected the section form with wrapping off:\n%s", out)
	}
	if strings.Contains(out, "{\n") {
		t.Errorf("a table was still broken across lines:\n%s", out)
	}
}

// Turning wrapping off does not turn inlining off: a table that fits its line is
// still an inline table.
func TestRender_InlineTable_WrapDisabled_StillInlinesWhatFits(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("buckets.*"), tomlparser.WithInlineWrap(false))
	ctx := render.WithTTYSizeCtx(context.Background(), 100, 0)
	out := renderPlain(ctx, t, p, bucketData(kongfig.ConfigData{"path": "/w"}))

	if !strings.Contains(out, `work = {path = "/w"}`) {
		t.Errorf("expected the one-line form:\n%s", out)
	}
}

// An array of tables reaches the same wrap from the other side: its elements are
// inline tables too, so with wrapping off an element that does not fit turns the
// array into [[blocks]] rather than spilling one entry across two lines.
func TestRender_TableArray_WrapDisabled_DemotesToBlocks(t *testing.T) {
	data := kongfig.ConfigData{
		"aux": []any{
			kongfig.ConfigData{
				"dir":    "research/package-managers/UseCarthageFramework",
				"parent": "some-team/some-ios-client",
			},
			kongfig.ConfigData{"dir": "a/b", "parent": "c/d"},
		},
	}

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.New(tomlparser.WithInlineWrap(false)), data)

	if !strings.Contains(out, "[[aux]]") {
		t.Errorf("expected the block form with wrapping off:\n%s", out)
	}
	if strings.Contains(out, "{\n") {
		t.Errorf("an element was still broken across lines:\n%s", out)
	}
}

// A written config file must not depend on the terminal that produced it.
func TestMarshal_InlineTable_NeverWraps(t *testing.T) {
	b, err := fieldsParser().Marshal(fieldsData())
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if strings.Contains(string(b), "{\n") {
		t.Errorf("marshal broke an inline table across lines:\n%s", b)
	}
}
