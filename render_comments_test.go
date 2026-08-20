package kongfig_test

import (
	"context"
	"io"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/render"
)

// renderCommentState renders kf with a capturing renderer and reports what the
// renderer's context said about provenance and help comments.
func renderCommentState(t *testing.T, opts ...kongfig.RenderOption) (noProvenance, noHelp bool) {
	t.Helper()
	kf := kongfig.New()
	if err := kf.LoadParsed(map[string]any{"host": "localhost"}, "file"); err != nil {
		t.Fatal("load:", err)
	}
	capture := &ctxCapture{}
	if err := kf.RenderWith(context.Background(), io.Discard, capture, opts...); err != nil {
		t.Fatal("render:", err)
	}
	return render.NoProvenance(capture.ctx), render.NoHelp(capture.ctx)
}

func TestWithRenderNoProvenance_SuppressesSourcesOnly(t *testing.T) {
	noProvenance, noHelp := renderCommentState(t, kongfig.WithRenderNoProvenance())

	if !noProvenance {
		t.Error("expected the renderer to see NoProvenance")
	}
	if noHelp {
		t.Error("WithRenderNoProvenance must leave help comments on")
	}
}

func TestWithRenderNoHelp_SuppressesHelpOnly(t *testing.T) {
	noProvenance, noHelp := renderCommentState(t, kongfig.WithRenderNoHelp())

	if !noHelp {
		t.Error("expected the renderer to see NoHelp")
	}
	if noProvenance {
		t.Error("WithRenderNoHelp must leave source annotations on")
	}
}

func TestWithRenderNoComments_SuppressesBoth(t *testing.T) {
	noProvenance, noHelp := renderCommentState(t, kongfig.WithRenderNoComments())

	if !noProvenance || !noHelp {
		t.Errorf("expected both suppressed, got noProvenance=%v noHelp=%v", noProvenance, noHelp)
	}
}

func TestRenderComments_OnByDefault(t *testing.T) {
	noProvenance, noHelp := renderCommentState(t)

	if noProvenance || noHelp {
		t.Errorf("expected comments on by default, got noProvenance=%v noHelp=%v", noProvenance, noHelp)
	}
}
