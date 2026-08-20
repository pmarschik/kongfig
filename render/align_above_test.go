package render_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/pmarschik/kongfig/render"
)

// alignAt returns the column each annotated line puts needle at, and -1 for a
// line that does not carry it.
func alignAt(out, needle string) []int {
	var cols []int
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		cols = append(cols, strings.Index(line, needle))
	}
	return cols
}

// One line too wide to be annotated at the column the others align on must not
// cost the whole document its inline annotations: the alignment column is the one
// that keeps the most of them, so the wide line is the one that gives way.
func TestAlignAnnotations_WideLineDoesNotEvictTheOthers(t *testing.T) {
	marker := render.AnnMarker
	wide := "sink = " + strings.Repeat("x", 93) // 100 columns of content
	raw := "host = 1" + marker + "  # file (env)\n" +
		"port = 2" + marker + "  # file (env)\n" +
		wide + marker + "  # defaults\n"

	ctx := render.WithTTYSizeCtx(context.Background(), 114, 0)
	var buf bytes.Buffer
	if err := render.AlignAnnotationsCtx(ctx, raw, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected the wide line's annotation above it and the rest inline, got:\n%s", out)
	}
	cols := alignAt(out, "# file (env)")
	if cols[0] < 0 || cols[1] < 0 {
		t.Errorf("a narrow line lost its inline annotation:\n%s", out)
	}
	if cols[0] != cols[1] {
		t.Errorf("annotations not aligned: col %d vs %d\n%s", cols[0], cols[1], out)
	}
	if lines[2] != "# defaults" || lines[3] != wide {
		t.Errorf("the wide line is the one that should give way:\n%s", out)
	}
	for _, line := range lines {
		if w := render.VisualWidth(line); w > 114 {
			t.Errorf("line runs past the terminal (%d cols): %q", w, line)
		}
	}
}

// The alignment column has to clear every inline line's content, so a line wide
// enough that only its own annotation would fit there still gives way when the
// narrower lines outnumber it.
func TestAlignAnnotations_ManyNarrowLinesOutvoteOneWideOne(t *testing.T) {
	marker := render.AnnMarker
	raw := "a = 1" + marker + "  # file\n" +
		"b = 2" + marker + "  # file\n" +
		"c = " + strings.Repeat("y", 26) + marker + "  # x\n"

	ctx := render.WithTTYSizeCtx(context.Background(), 36, 0)
	var buf bytes.Buffer
	if err := render.AlignAnnotationsCtx(ctx, raw, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 || lines[2] != "# x" {
		t.Fatalf("the two narrow lines should have kept their annotations:\n%s", out)
	}
}
