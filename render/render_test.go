package render_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/pmarschik/kongfig/render"
)

func TestAlignAnnotations_RightAligned(t *testing.T) {
	// With cols=40 and annotation "  # src" (7 chars wide), annotations should
	// start at col 33 (= 40 - 7), not just one space after the longest value.
	marker := render.AnnMarker
	raw := "short = 1" + marker + "  # src\n" +
		"longer_key = 2" + marker + "  # src\n"

	ctx := render.WithTTYSizeCtx(context.Background(), 40, 0)
	var buf bytes.Buffer
	if err := render.AlignAnnotationsCtx(ctx, raw, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), out)
	}

	col0 := strings.Index(lines[0], "  # src")
	col1 := strings.Index(lines[1], "  # src")
	if col0 != col1 {
		t.Errorf("annotations not aligned: col %d vs %d\n%s", col0, col1, out)
	}
	// cols=40, annotation width 7 → alignCol = max(15, 33) = 33.
	if col0 != 33 {
		t.Errorf("expected annotation at col 33 (right-aligned in 40 cols), got col %d\n%s", col0, out)
	}
}

func TestAlignAnnotations_NoCols_LeftAligned(t *testing.T) {
	// Without cols (0), annotations align immediately after the longest content.
	marker := render.AnnMarker
	raw := "short = 1" + marker + "  # src\n" +
		"longer_key = 2" + marker + "  # src\n"

	var buf bytes.Buffer
	if err := render.AlignAnnotations(raw, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	col0 := strings.Index(lines[0], "  # src")
	col1 := strings.Index(lines[1], "  # src")
	if col0 != col1 {
		t.Errorf("annotations not aligned: col %d vs %d\n%s", col0, col1, out)
	}
	// No cols: alignCol = maxInlineVW+1 = 15 ("longer_key = 2" is 14 chars).
	if col0 != 15 {
		t.Errorf("expected annotation at col 15 (no-cols left-align), got col %d\n%s", col0, out)
	}
}
