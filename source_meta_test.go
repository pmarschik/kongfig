package kongfig_test

import (
	"context"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
)

// tracingStyler records which Styler methods were called and with what args.
type tracingStyler struct {
	sourceKindCalls []string
	sourceDataCalls []string
	sourceKeyCalls  []string
}

func (*tracingStyler) Key(v string) string           { return v }
func (*tracingStyler) String(v string) string        { return v }
func (*tracingStyler) Number(v string) string        { return v }
func (*tracingStyler) Bool(v string) string          { return v }
func (*tracingStyler) Null(v string) string          { return v }
func (*tracingStyler) Syntax(v string) string        { return v }
func (*tracingStyler) Comment(v string) string       { return v }
func (*tracingStyler) Annotation(_, v string) string { return v }
func (*tracingStyler) Redacted(v string) string      { return v }
func (*tracingStyler) Codec(v string) string         { return v }

func (s *tracingStyler) SourceKind(v string) string {
	s.sourceKindCalls = append(s.sourceKindCalls, v)
	return "[kind:" + v + "]"
}

func (s *tracingStyler) SourceData(v string) string {
	s.sourceDataCalls = append(s.sourceDataCalls, v)
	return "[data:" + v + "]"
}

func (s *tracingStyler) SourceKey(v string) string {
	s.sourceKeyCalls = append(s.sourceKeyCalls, v)
	return "[key:" + v + "]"
}

// testProviderData is a minimal ProviderData that returns a pre-styled string.
type testProviderData struct{ val string }

func (d testProviderData) RenderAnnotation(_ context.Context, s kongfig.Styler, _ string) string {
	return s.SourceData(d.val)
}

// testProviderKey uses SourceKey styling instead.
type testProviderKey struct{ val string }

func (d testProviderKey) RenderAnnotation(_ context.Context, s kongfig.Styler, _ string) string {
	if d.val == "" {
		return ""
	}
	return s.SourceKey(d.val)
}

func TestLayerMetaRenderAnnotation_WithData(t *testing.T) {
	s := &tracingStyler{}
	meta := kongfig.LayerMeta{Kind: "file", Data: testProviderData{val: "/path/to/config.yaml"}}

	got := meta.RenderAnnotation(context.Background(), s, "")

	want := "[kind:file] ([data:/path/to/config.yaml])"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(s.sourceKindCalls) != 1 || s.sourceKindCalls[0] != "file" {
		t.Errorf("SourceKind calls: %v", s.sourceKindCalls)
	}
	if len(s.sourceDataCalls) != 1 || s.sourceDataCalls[0] != "/path/to/config.yaml" {
		t.Errorf("SourceData calls: %v", s.sourceDataCalls)
	}
}

func TestLayerMetaRenderAnnotation_EmptyData(t *testing.T) {
	s := &tracingStyler{}
	meta := kongfig.LayerMeta{Kind: "env", Data: testProviderKey{val: ""}}

	got := meta.RenderAnnotation(context.Background(), s, "")

	// empty data → no parens, just the kind
	want := "[kind:env]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(s.sourceKeyCalls) != 0 {
		t.Errorf("SourceKey should not be called for empty data: %v", s.sourceKeyCalls)
	}
}

func TestLayerMetaRenderAnnotation_WithSourceKey(t *testing.T) {
	s := &tracingStyler{}
	meta := kongfig.LayerMeta{Kind: "env", Data: testProviderKey{val: "$APP_DB_URL"}}

	got := meta.RenderAnnotation(context.Background(), s, "db.url")

	want := "[kind:env] ([key:$APP_DB_URL])"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(s.sourceKindCalls) != 1 || s.sourceKindCalls[0] != "env" {
		t.Errorf("SourceKind calls: %v", s.sourceKindCalls)
	}
	if len(s.sourceKeyCalls) != 1 || s.sourceKeyCalls[0] != "$APP_DB_URL" {
		t.Errorf("SourceKey calls: %v", s.sourceKeyCalls)
	}
}

func TestLayerMetaRenderAnnotation_PanicOnMultiline(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for multiline RenderAnnotation output")
		}
	}()

	s := &tracingStyler{}
	meta := kongfig.LayerMeta{Kind: "file", Data: testProviderData{val: "line1\nline2"}}
	meta.RenderAnnotation(context.Background(), s, "")
}

func TestRenderAnnotation_WithLayerMeta(t *testing.T) {
	s := &tracingStyler{}
	meta := kongfig.LayerMeta{Kind: "file", Data: testProviderData{val: "/etc/app/config.yaml"}}
	rv := kongfig.RenderedValue{Source: kongfig.SourceMeta{Layer: meta}}

	got := render.Annotation(context.Background(), rv, "host", s)

	want := "[kind:file] ([data:/etc/app/config.yaml])"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
