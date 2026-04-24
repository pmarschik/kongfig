package kongfig_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
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

func TestYAMLRenderer_AlignSources(t *testing.T) {
	kf := kongfig.New()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"host": "localhost", "log-level": "info", "port": 8080},
		source: "defaults",
	})

	r := yamlparser.Default.Bind(plainStyler{})
	var buf bytes.Buffer
	if err := kf.RenderWith(context.Background(), &buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// Collect annotation columns.
	var cols []int
	for _, line := range lines {
		if idx := strings.Index(line, "  # "); idx >= 0 {
			cols = append(cols, idx)
		}
	}
	if len(cols) < 2 {
		t.Skipf("fewer than 2 annotated lines, skipping:\n%s", out)
	}
	for i, c := range cols[1:] {
		if c != cols[0] {
			t.Errorf("annotation column mismatch: line0=%d, line%d=%d\n%s", cols[0], i+1, c, out)
		}
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
