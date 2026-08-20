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

// The annotation can only ride the line that closes the string: a line-ending
// backslash cannot carry a comment, and an annotation the aligner cannot fit goes
// above its line — which for a folded string is a line inside the string, where the
// backslash above it swallows the comment into the value. So the fold has to leave
// the annotation room on its closing line, at every terminal width.
func TestRender_FoldedString_KeepsTheAnnotationOnTheClosingLine(t *testing.T) {
	data := kongfig.ConfigData{"description": sourced(longDescription, "workdir-config-file")}

	for cols := 48; cols <= 96; cols++ {
		ctx := render.WithTTYSizeCtx(context.Background(), cols, 0)
		out := renderPlain(ctx, t, tomlparser.Default, data)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

		for _, line := range lines {
			if render.VisualWidth(line) > cols {
				t.Errorf("cols=%d: line runs past the terminal (%d cols): %q",
					cols, render.VisualWidth(line), line)
			}
		}
		if last := lines[len(lines)-1]; !strings.Contains(last, `"""`) ||
			!strings.Contains(last, "# workdir-config-file") {
			t.Errorf("cols=%d: annotation is not on the closing line:\n%s", cols, out)
		}

		back, err := tomlparser.Default.Unmarshal([]byte(out))
		if err != nil {
			t.Fatalf("cols=%d: folded string did not reparse: %v\n%s", cols, err, out)
		}
		if back["description"] != longDescription {
			t.Errorf("cols=%d: reparsed value = %q, want the original\n%s",
				cols, back["description"], out)
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

// The closing line is the only line the annotation may follow, so the fold has to
// leave room for it there: a fold packed to the full width would push the
// provenance onto a line of its own above the entry.
func TestRender_FoldedString_LeavesRoomForTheAnnotation(t *testing.T) {
	data := kongfig.ConfigData{"description": sourced(longDescription, "file (~/.config/app.toml)")}

	ctx := render.WithTTYSizeCtx(context.Background(), 80, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.Contains(lines[len(lines)-1], "# file (~/.config/app.toml)") {
		t.Errorf("annotation did not stay on the entry:\n%s", out)
	}
	for _, line := range lines {
		if w := render.VisualWidth(line); w > 80 {
			t.Errorf("line runs past the terminal (%d cols): %q", w, line)
		}
	}
	if back, err := tomlparser.Default.Unmarshal([]byte(out)); err != nil {
		t.Fatalf("folded string did not reparse: %v\n%s", err, out)
	} else if back["description"] != longDescription {
		t.Errorf("reparsed value = %q, want the original\n%s", back["description"], out)
	}
}

// The annotation rides the opening bracket, and the elements below it belong to
// the same entry: they are folded to the same width, so the block stays clear of
// the column the annotation sits in.
func TestRender_ArrayBlock_FoldedElementStaysClearOfTheAnnotation(t *testing.T) {
	data := kongfig.ConfigData{"remove": sourced([]any{longDescription}, "file ($xdg)")}

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	annCol := strings.Index(lines[0], "# file ($xdg)")
	if annCol < 0 {
		t.Fatalf("annotation is not beside the bracket:\n%s", out)
	}
	for _, line := range lines[1:] {
		if w := render.VisualWidth(line); w > annCol {
			t.Errorf("element line reaches into the annotation column (%d > %d): %q", w, annCol, line)
		}
	}
}

// The lines of a folded string are the value; a comment written above the line
// that closes it is string content, and the document reparses to something else.
// So the annotation of a folded string is pinned to that line however narrow the
// terminal, even at the cost of the alignment column.
func TestRender_FoldedString_AnnotationIsNeverLiftedIntoTheString(t *testing.T) {
	data := kongfig.ConfigData{
		"description": sourced(longDescription, "file (~/.config/a-long-file-name.toml)"),
		"short":       sourced("x", "defaults"),
	}

	ctx := render.WithTTYSizeCtx(context.Background(), 60, 0)
	out := renderPlain(ctx, t, tomlparser.Default, data)

	if !strings.Contains(out, "# file (~/.config/a-long-file-name.toml)") {
		t.Errorf("the folded entry lost its provenance:\n%s", out)
	}
	// A comment the reader sees is a comment the parser skips; one that landed
	// inside the string is part of the value instead.
	back, err := tomlparser.Default.Unmarshal([]byte(out))
	if err != nil {
		t.Fatalf("rendered document did not reparse: %v\n%s", err, out)
	}
	if back["description"] != longDescription {
		t.Errorf("reparsed value = %q, want the original\n%s", back["description"], out)
	}
}
