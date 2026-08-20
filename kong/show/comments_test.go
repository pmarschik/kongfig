package show_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
	kongshow "github.com/pmarschik/kongfig/kong/show"
	"github.com/pmarschik/kongfig/render"
	"github.com/pmarschik/kongfig/style/plain"
)

// commentRecorder is a renderer that records what its context said about
// provenance and help comments. Used as Flags.DefaultRenderer so the recording
// happens on the real render path.
type commentRecorder struct {
	noProvenance bool
	noHelp       bool
}

func (r *commentRecorder) Bind(kongfig.Styler) kongfig.Renderer { return r }

func (r *commentRecorder) Render(ctx context.Context, _ io.Writer, _ kongfig.ConfigData) error {
	r.noProvenance = render.NoProvenance(ctx)
	r.noHelp = render.NoHelp(ctx)
	return nil
}

func recordComments(t *testing.T, f *kongshow.Flags) *commentRecorder {
	t.Helper()
	rec := &commentRecorder{}
	f.DefaultRenderer = rec
	k := newKongfig(t, map[string]any{"host": "localhost"})
	if err := f.Render(context.Background(), io.Discard, k, plain.New()); err != nil {
		t.Fatal("render:", err)
	}
	return rec
}

func TestFlags_NoProvenance_SuppressesSourcesOnly(t *testing.T) {
	rec := recordComments(t, &kongshow.Flags{
		CommentsFlag: kongshow.CommentsFlag{NoProvenance: true},
	})

	if !rec.noProvenance {
		t.Error("--no-provenance did not reach the renderer")
	}
	if rec.noHelp {
		t.Error("--no-provenance must leave help comments on")
	}
}

func TestFlags_NoHelp_SuppressesHelpOnly(t *testing.T) {
	rec := recordComments(t, &kongshow.Flags{
		CommentsFlag: kongshow.CommentsFlag{NoHelp: true},
	})

	if !rec.noHelp {
		t.Error("--no-help did not reach the renderer")
	}
	if rec.noProvenance {
		t.Error("--no-help must leave source annotations on")
	}
}

func TestFlags_NoComments_SuppressesBoth(t *testing.T) {
	rec := recordComments(t, &kongshow.Flags{
		CommentsFlag: kongshow.CommentsFlag{NoComments: true},
	})

	if !rec.noProvenance || !rec.noHelp {
		t.Errorf("--no-comments must suppress both, got noProvenance=%v noHelp=%v", rec.noProvenance, rec.noHelp)
	}
}

func TestFlags_Default_KeepsComments(t *testing.T) {
	rec := recordComments(t, &kongshow.Flags{})

	if rec.noProvenance || rec.noHelp {
		t.Errorf("comments must be on by default, got noProvenance=%v noHelp=%v", rec.noProvenance, rec.noHelp)
	}
}

func TestSimpleFlags_NoProvenance_ReachesRenderer(t *testing.T) {
	rec := &commentRecorder{}
	f := &kongshow.SimpleFlags{
		DefaultRenderer: rec,
		CommentsFlag:    kongshow.CommentsFlag{NoProvenance: true},
	}
	k := newKongfig(t, map[string]any{"host": "localhost"})
	if err := f.Render(context.Background(), io.Discard, k, plain.New()); err != nil {
		t.Fatal("render:", err)
	}

	if !rec.noProvenance {
		t.Error("--no-provenance did not reach the renderer")
	}
}

// TestFlagsRenderer_NoProvenance keeps the flags-format renderer honest: it is the
// one renderer that lives in this package, so its annotations must obey the flag.
func TestFlagsRenderer_NoProvenance(t *testing.T) {
	k := newKongfig(t, map[string]any{"host": "localhost"})

	var withSources bytes.Buffer
	f := &kongshow.Flags{FormatFlag: kongshow.FormatFlag{Format: "flags"}}
	if err := f.Render(context.Background(), &withSources, k, plain.New()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withSources.String(), "#") {
		t.Fatalf("expected a source annotation to start with:\n%s", withSources.String())
	}

	var noSources bytes.Buffer
	f = &kongshow.Flags{
		FormatFlag:   kongshow.FormatFlag{Format: "flags"},
		CommentsFlag: kongshow.CommentsFlag{NoProvenance: true},
	}
	if err := f.Render(context.Background(), &noSources, k, plain.New()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(noSources.String(), "#") {
		t.Errorf("expected no source annotation under --no-provenance:\n%s", noSources.String())
	}
	if !strings.Contains(noSources.String(), "--host") {
		t.Errorf("expected the value itself to survive:\n%s", noSources.String())
	}
}

// --- --layers conflict ---

type showCLI struct {
	Show showCmd `cmd:""`
}

type showCmd struct {
	kongshow.Flags `embed:""`
}

func (showCmd) Run() error { return nil }

func parseShow(t *testing.T, args ...string) error {
	t.Helper()
	var cli showCLI
	parser, err := kong.New(&cli, kongshow.FlagsVars(), kong.Exit(func(int) {}), kong.Writers(io.Discard, io.Discard))
	if err != nil {
		t.Fatal("kong.New:", err)
	}
	_, err = parser.Parse(args)
	return err
}

func TestFlags_NoProvenanceWithLayers_IsRejected(t *testing.T) {
	err := parseShow(t, "show", "--layers", "--no-provenance")
	if err == nil {
		t.Fatal("expected --no-provenance with --layers to be rejected")
	}
	if !strings.Contains(err.Error(), "--no-provenance") || !strings.Contains(err.Error(), "--layers") {
		t.Errorf("error should name both flags, got: %v", err)
	}
}

func TestFlags_NoProvenanceWithoutLayers_IsAccepted(t *testing.T) {
	if err := parseShow(t, "show", "--no-provenance"); err != nil {
		t.Fatalf("--no-provenance alone must be accepted: %v", err)
	}
}

func TestFlags_NoHelpWithLayers_IsAccepted(t *testing.T) {
	// Help comments are per-value in every layer, so hiding them stays meaningful
	// in the per-layer view; only provenance is redundant there.
	if err := parseShow(t, "show", "--layers", "--no-help"); err != nil {
		t.Fatalf("--no-help with --layers must be accepted: %v", err)
	}
}

func TestSimpleFlags_NoProvenanceWithLayers_IsRejected(t *testing.T) {
	f := kongshow.SimpleFlags{
		LayersFlag:   kongshow.LayersFlag{Layers: true},
		CommentsFlag: kongshow.CommentsFlag{NoProvenance: true},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected --no-provenance with --layers to be rejected")
	}
}
