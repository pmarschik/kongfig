package kongfig_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	envprovider "github.com/pmarschik/kongfig/providers/env"
	render "github.com/pmarschik/kongfig/render"
)

func TestAlignAnnotations_Basic(t *testing.T) {
	raw := "host: localhost" + render.AnnMarker + "  # defaults\n" +
		"log-level: info" + render.AnnMarker + "  # env\n" +
		"port: 8080\n"
	var buf bytes.Buffer
	if err := render.AlignAnnotations(raw, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	// Both annotated lines should have their annotations at the same column.
	col0 := strings.Index(lines[0], "  # ")
	col1 := strings.Index(lines[1], "  # ")
	if col0 != col1 {
		t.Errorf("annotation columns differ: line0=%d, line1=%d\nlines:\n%s", col0, col1, buf.String())
	}
	// Unannotated line should be unchanged.
	if lines[2] != "port: 8080" {
		t.Errorf("unannotated line changed: %q", lines[2])
	}
}

func TestAlignAnnotations_NoAnnotations(t *testing.T) {
	raw := "host: localhost\nport: 8080\n"
	var buf bytes.Buffer
	if err := render.AlignAnnotations(raw, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "host: localhost\nport: 8080\n" {
		t.Errorf("unexpected output: %q", buf.String())
	}
}

func TestVisualWidth_StripANSI(t *testing.T) {
	styled := "\x1b[32mhello\x1b[0m"
	if w := render.VisualWidth(styled); w != 5 {
		t.Errorf("VisualWidth(%q) = %d, want 5", styled, w)
	}
	if w := render.VisualWidth("hello"); w != 5 {
		t.Errorf("VisualWidth(plain) = %d, want 5", w)
	}
}

func TestEnvRenderer_AlignSources(t *testing.T) {
	kf := kongfig.New()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"host": "localhost", "log-level": "info"},
		source: "defaults",
	})

	p := envprovider.Provider("APP", "_")
	r := p.Bind(plainStyler{})
	var buf bytes.Buffer
	if err := kf.RenderWith(context.Background(), &buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var cols []int
	for _, line := range lines {
		if idx := strings.Index(line, "  # "); idx >= 0 {
			cols = append(cols, idx)
		}
	}
	if len(cols) < 2 {
		t.Skipf("fewer than 2 annotated lines:\n%s", out)
	}
	for i, c := range cols[1:] {
		if c != cols[0] {
			t.Errorf("annotation column mismatch: line0=%d, line%d=%d\n%s", cols[0], i+1, c, out)
		}
	}
}

func TestAlignAnnotationsCtx_AboveLine(t *testing.T) {
	// With a narrow terminal (cols=20), annotations that would overflow inline
	// should be placed as comment lines above the value line.
	raw := "host: localhost" + render.AnnMarker + "  # defaults\n" +
		"port: 8080" + render.AnnMarker + "  # defaults\n"

	// "host: localhost" is 15 chars; "  # defaults" is 12 chars: 15+1+12=28 > 20.
	ctx := render.TTYSizeKey.WithCtx(context.Background(), render.TTYSize{Cols: 20})

	var buf bytes.Buffer
	if err := render.AlignAnnotationsCtx(ctx, raw, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	// Expect 4 lines: annotation + value for each of the 2 entries.
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (annotation above each value), got %d:\n%s", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], "# ") {
		t.Errorf("line 0 should be annotation comment, got %q", lines[0])
	}
	if lines[1] != "host: localhost" {
		t.Errorf("line 1 should be value line, got %q", lines[1])
	}
}

func TestAlignAnnotationsCtx_InlineWhenWideEnough(t *testing.T) {
	// With a wide terminal, inline alignment should be used as normal.
	raw := "host: localhost" + render.AnnMarker + "  # defaults\n" +
		"port: 8080" + render.AnnMarker + "  # defaults\n"

	ctx := render.TTYSizeKey.WithCtx(context.Background(), render.TTYSize{Cols: 120})

	var buf bytes.Buffer
	if err := render.AlignAnnotationsCtx(ctx, raw, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 inline lines, got %d:\n%s", len(lines), buf.String())
	}
	// Annotations should be aligned at the same column.
	col0 := strings.Index(lines[0], "# defaults")
	col1 := strings.Index(lines[1], "# defaults")
	if col0 != col1 {
		t.Errorf("annotation columns differ: line0=%d, line1=%d\n%s", col0, col1, buf.String())
	}
}
