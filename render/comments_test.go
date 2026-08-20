package render_test

import (
	"context"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/render"
	"github.com/pmarschik/kongfig/style/plain"
)

func sourced(v any) kongfig.RenderedValue {
	return kongfig.RenderedValue{
		Value: v,
		Source: kongfig.SourceMeta{
			Layer: kongfig.LayerMeta{Kind: kongfig.KindFile, Name: "file"},
		},
	}
}

func helpCtx(ctx context.Context) context.Context {
	return kongfig.WithRenderHelpTextsCtx(ctx, map[string]string{"host": "The host to bind."})
}

func TestNoProvenance_SuppressesAnnotationKeepsHelp(t *testing.T) {
	ctx := helpCtx(kongfig.WithRenderNoProvenanceCtx(context.Background()))
	s := plain.New()

	if ann := render.Annotation(ctx, sourced("localhost"), "host", s); ann != "" {
		t.Errorf("expected no source annotation under NoProvenance, got %q", ann)
	}
	if help := render.HelpText(ctx, "host"); help != "The host to bind." {
		t.Errorf("expected help text to survive NoProvenance, got %q", help)
	}
}

func TestNoHelp_SuppressesHelpKeepsAnnotation(t *testing.T) {
	ctx := helpCtx(kongfig.WithRenderNoHelpCtx(context.Background()))
	s := plain.New()

	if help := render.HelpText(ctx, "host"); help != "" {
		t.Errorf("expected no help text under NoHelp, got %q", help)
	}
	if texts := render.HelpTexts(ctx); len(texts) != 0 {
		t.Errorf("expected no help texts under NoHelp, got %v", texts)
	}
	if ann := render.Annotation(ctx, sourced("localhost"), "host", s); ann == "" {
		t.Error("expected the source annotation to survive NoHelp")
	}
}

func TestNoComments_ImpliesBoth(t *testing.T) {
	ctx := helpCtx(kongfig.WithRenderNoCommentsCtx(context.Background()))

	if !render.NoProvenance(ctx) {
		t.Error("expected NoProvenance to report true under NoComments")
	}
	if !render.NoHelp(ctx) {
		t.Error("expected NoHelp to report true under NoComments")
	}
}

func TestNoProvenanceAndNoHelp_ReportThemselves(t *testing.T) {
	base := context.Background()

	if render.NoProvenance(base) || render.NoHelp(base) {
		t.Fatal("expected comments to be on by default")
	}
	if !render.NoProvenance(kongfig.WithRenderNoProvenanceCtx(base)) {
		t.Error("expected NoProvenance under WithRenderNoProvenanceCtx")
	}
	if render.NoHelp(kongfig.WithRenderNoProvenanceCtx(base)) {
		t.Error("NoProvenance must not suppress help")
	}
	if !render.NoHelp(kongfig.WithRenderNoHelpCtx(base)) {
		t.Error("expected NoHelp under WithRenderNoHelpCtx")
	}
	if render.NoProvenance(kongfig.WithRenderNoHelpCtx(base)) {
		t.Error("NoHelp must not suppress provenance")
	}
}
