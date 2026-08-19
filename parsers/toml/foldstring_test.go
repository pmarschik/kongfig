package toml_test

import (
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	"github.com/pmarschik/kongfig/render"
)

const longDescription = "the archive keeps every build we shipped, so a restore " +
	"can start from any release without asking the release team first"

// A string too wide for the terminal folds into a multi-line basic string rather
// than being left to the terminal, which breaks mid-token at column zero.
func TestRender_LongString_FoldsAcrossLines(t *testing.T) {
	data := kongfig.ConfigData{"description": longDescription}

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if !strings.HasPrefix(out, `description = """`) {
		t.Fatalf("string was not folded:\n%s", out)
	}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if render.VisualWidth(line) > 60 {
			t.Errorf("line runs past the terminal (%d cols): %q", render.VisualWidth(line), line)
		}
	}
}

// The fold is a line break in the file, not in the value: a line-ending backslash
// trims the newline and the indentation that follows it.
func TestRender_FoldedString_ReparsesUnchanged(t *testing.T) {
	data := kongfig.ConfigData{"description": longDescription}

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)
	if !strings.Contains(out, "\\\n") {
		t.Fatalf("expected a folded string; nothing folded:\n%s", out)
	}

	back, err := tomlparser.Default.Unmarshal([]byte(out))
	if err != nil {
		t.Fatalf("folded string did not reparse: %v\n%s", err, out)
	}
	if back["description"] != longDescription {
		t.Errorf("reparsed value = %q, want the original\n%s", back["description"], out)
	}
}

// A value with no place to break stays on its one long line: a fold in the middle
// of a token would be a fold the reader cannot see.
func TestRender_LongStringWithoutSpaces_StaysOnOneLine(t *testing.T) {
	url := "https://example.invalid/a/very/long/path/that/keeps/going/and/going/further"
	data := kongfig.ConfigData{"endpoint": url}

	ctx := render.WithTTYSizeCtx(context.Background(), 40, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if !strings.Contains(out, `endpoint = "`+url+`"`) {
		t.Errorf("unbreakable value was not left alone:\n%s", out)
	}
}

// An element of an expanded array is a value on a line like any other, so a
// string too wide for that line folds there too.
func TestRender_ArrayBlock_FoldsLongStringElements(t *testing.T) {
	data := kongfig.ConfigData{"remove": []any{longDescription, "short"}}

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if render.VisualWidth(line) > 60 {
			t.Errorf("line runs past the terminal (%d cols): %q", render.VisualWidth(line), line)
		}
	}

	back, err := tomlparser.Default.Unmarshal([]byte(out))
	if err != nil {
		t.Fatalf("folded element did not reparse: %v\n%s", err, out)
	}
	elems, ok := back["remove"].([]any)
	if !ok || len(elems) != 2 {
		t.Fatalf("remove = %#v, want two elements\n%s", back["remove"], out)
	}
	if elems[0] != longDescription || elems[1] != "short" {
		t.Errorf("reparsed elements = %#v, want the originals\n%s", elems, out)
	}
}

// A written config file must not depend on the terminal that produced it.
func TestMarshal_NeverFoldsStrings(t *testing.T) {
	b, err := tomlparser.Default.Marshal(kongfig.ConfigData{"description": longDescription})
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if strings.Contains(string(b), `"""`) {
		t.Errorf("marshal folded a string:\n%s", b)
	}
}

// A comment cannot live inside a string, so the provenance follows the closing
// delimiter on the last line.
func TestRender_FoldedString_AnnotationFollowsTheClosingDelimiter(t *testing.T) {
	data := kongfig.ConfigData{"description": sourced(longDescription, "file")}

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, `"""`) || !strings.Contains(last, "# file") {
		t.Errorf("annotation is not on the closing line:\n%s", out)
	}
	for _, line := range lines[:len(lines)-1] {
		if strings.Contains(line, "#") {
			t.Errorf("a comment landed inside the string:\n%s", out)
		}
	}
}

// A redacted value carries a placeholder, not the value it hides; folding it
// would fold the placeholder.
func TestRender_FoldedString_LeavesRedactedAlone(t *testing.T) {
	data := kongfig.ConfigData{
		"token": kongfig.RenderedValue{
			Value: longDescription, Redacted: true, RedactedDisplay: "********",
		},
	}

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if strings.Contains(out, `"""`) {
		t.Errorf("redacted placeholder was folded:\n%s", out)
	}
}
