package show_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	kongshow "github.com/pmarschik/kongfig/kong/show"
	"github.com/pmarschik/kongfig/style/plain"
)

func newKongfig(t *testing.T, data map[string]any, source string) *kongfig.Kongfig {
	t.Helper()
	k := kongfig.New()
	if err := k.LoadParsed(data, source); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRenderYAML(t *testing.T) {
	k := newKongfig(t, map[string]any{"host": "localhost", "port": 8080}, "file")
	f := &kongshow.Flags{}
	var buf bytes.Buffer
	if err := f.Render(context.Background(), &buf, k, plain.New(), kongfig.WithRenderNoComments()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "host") {
		t.Errorf("expected 'host' in output:\n%s", out)
	}
}

func TestRenderEnv(t *testing.T) {
	k := newKongfig(t, map[string]any{"host": "localhost"}, "file")
	f := &kongshow.Flags{FormatFlag: kongshow.FormatFlag{Format: "env"}}
	var buf bytes.Buffer
	if err := f.Render(context.Background(), &buf, k, plain.New(), kongfig.WithRenderNoComments()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "export") {
		t.Errorf("expected 'export' in output:\n%s", out)
	}
}

func TestRenderFlags(t *testing.T) {
	k := newKongfig(t, map[string]any{"host": "localhost"}, "file")
	f := &kongshow.Flags{FormatFlag: kongshow.FormatFlag{Format: "flags"}}
	var buf bytes.Buffer
	if err := f.Render(context.Background(), &buf, k, plain.New(), kongfig.WithRenderNoComments()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "--host") {
		t.Errorf("expected '--host' in output:\n%s", out)
	}
}

func TestRenderLayers(t *testing.T) {
	k := kongfig.New()
	if err := k.LoadParsed(map[string]any{"host": "default"}, "defaults"); err != nil {
		t.Fatal(err)
	}
	if err := k.LoadParsed(map[string]any{"host": "filehost"}, "file"); err != nil {
		t.Fatal(err)
	}
	f := &kongshow.Flags{LayersFlag: kongshow.LayersFlag{Layers: true}}
	var buf bytes.Buffer
	if err := f.Render(context.Background(), &buf, k, plain.New(), kongfig.WithRenderNoComments()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "defaults") {
		t.Errorf("expected 'defaults' section header:\n%s", out)
	}
	if !strings.Contains(out, "file") {
		t.Errorf("expected 'file' section header:\n%s", out)
	}
}

// loadParsedWithSource uses LoadParsed indirectly via a helper that sets a source label.
// LoadParsed only takes source, so env sub-sources are loaded by loading twice with different source labels.
func loadSource(t *testing.T, k *kongfig.Kongfig, data map[string]any, source string) {
	t.Helper()
	if err := k.LoadParsed(data, source); err != nil {
		t.Fatal(err)
	}
}

// TestLayerHeaderVerboseEnv asserts that --layers -v shows the full env sub-source label
// (e.g. "env.tag", "env.kong") rather than the collapsed "env" kind.
func TestLayerHeaderVerboseEnv(t *testing.T) {
	k := kongfig.New()
	loadSource(t, k, map[string]any{"host": "fromtag"}, "env.tag")
	loadSource(t, k, map[string]any{"port": "8080"}, "env.kong")

	f := &kongshow.Flags{LayersFlag: kongshow.LayersFlag{Layers: true, Verbose: 1}}
	var buf bytes.Buffer
	if err := f.Render(context.Background(), &buf, k, plain.New()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "env.tag") {
		t.Errorf("expected full sub-source label 'env.tag' in --layers -v output:\n%s", out)
	}
	if !strings.Contains(out, "env.kong") {
		t.Errorf("expected full sub-source label 'env.kong' in --layers -v output:\n%s", out)
	}
}

// TestLayerHeaderNonVerboseEnvGrouped asserts that --layers (non-verbose) groups env layers
// into a single "env" section header.
func TestLayerHeaderNonVerboseEnvGrouped(t *testing.T) {
	k := kongfig.New()
	loadSource(t, k, map[string]any{"host": "fromtag"}, "env.tag")
	loadSource(t, k, map[string]any{"port": "8080"}, "env.kong")

	f := &kongshow.Flags{LayersFlag: kongshow.LayersFlag{Layers: true}}
	var buf bytes.Buffer
	if err := f.Render(context.Background(), &buf, k, plain.New()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Non-verbose groups env.* into a single "env" layer
	if strings.Contains(out, "env.tag") || strings.Contains(out, "env.kong") {
		t.Errorf("expected env sub-sources to be grouped, not shown individually:\n%s", out)
	}
}

// TestLayerHeaderFileWithPath asserts that file layers with a DisplayPath still show
// the path in the header (meta renders more than just the kind name).
func TestLayerHeaderFileWithPath(t *testing.T) {
	k := kongfig.New()
	// Simulate a file layer with Meta carrying a display path by loading via LoadParsed
	// and then checking annotations; full path rendering tested in file_test.go.
	loadSource(t, k, map[string]any{"host": "filehost"}, "xdg.yaml")

	f := &kongshow.Flags{LayersFlag: kongshow.LayersFlag{Layers: true}}
	var buf bytes.Buffer
	if err := f.Render(context.Background(), &buf, k, plain.New()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// The source label should appear (no meta set for LoadParsed layers, falls back to Source)
	if !strings.Contains(out, "xdg.yaml") {
		t.Errorf("expected 'xdg.yaml' in layer header:\n%s", out)
	}
}

func TestFlagsVars(t *testing.T) {
	vars := kongshow.FlagsVars()
	if vars["kongrender_formats"] == "" {
		t.Error("expected non-empty kongrender_formats")
	}
}
