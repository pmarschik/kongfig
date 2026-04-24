package kongfig_test

import (
	"context"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	render "github.com/pmarschik/kongfig/render"
)

// --- RenderValue ---

func TestRenderValue_String(t *testing.T) {
	s := mockStyler{}
	got := render.Value(s, "hello", "hello")
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestRenderValue_Int(t *testing.T) {
	s := mockStyler{}
	got := render.Value(s, 42, "42")
	if got != "42" {
		t.Errorf("got %q, want %q", got, "42")
	}
}

func TestRenderValue_Bool(t *testing.T) {
	s := mockStyler{}
	got := render.Value(s, true, "true")
	if got != "true" {
		t.Errorf("got %q, want %q", got, "true")
	}
}

func TestRenderValue_RenderedValueRedacted(t *testing.T) {
	s := mockStyler{}
	rv := kongfig.RenderedValue{Redacted: true, RedactedDisplay: "***"}
	got := render.Value(s, rv, "should-be-ignored")
	if got != "***" {
		t.Errorf("got %q, want %q", got, "***")
	}
}

// --- PrepareRender: redaction ---

type redactConfig struct {
	Password string `kongfig:"password,redacted"`
	Host     string `kongfig:"host"`
}

func TestPrepareRender_Redaction(t *testing.T) {
	ctx := context.Background()
	kf := kongfig.NewFor[redactConfig]()
	kf.MustLoad(ctx, structsprovider.Defaults(redactConfig{Host: "localhost", Password: "secret"}))

	data, _ := kongfig.PrepareRender(ctx, kf)

	// password should be wrapped and redacted
	rv, ok := data["password"].(kongfig.RenderedValue)
	if !ok {
		t.Fatalf("expected RenderedValue for password, got %T", data["password"])
	}
	if !rv.Redacted {
		t.Error("expected password to be redacted")
	}
	if rv.RedactedDisplay == "secret" {
		t.Error("expected password not to show raw value")
	}
	// host should be wrapped but not redacted
	rvHost, ok := data["host"].(kongfig.RenderedValue)
	if !ok {
		t.Fatalf("expected RenderedValue for host, got %T", data["host"])
	}
	if rvHost.Redacted {
		t.Error("expected host not to be redacted")
	}
}

func TestPrepareRender_ShowRedacted(t *testing.T) {
	ctx := context.Background()
	kf := kongfig.NewFor[redactConfig]()
	kf.MustLoad(ctx, structsprovider.Defaults(redactConfig{Host: "localhost", Password: "secret"}))

	data, _ := kongfig.PrepareRender(ctx, kf, kongfig.WithRenderShowRedacted())

	rv, ok := data["password"].(kongfig.RenderedValue)
	if !ok {
		t.Fatalf("expected RenderedValue, got %T", data["password"])
	}
	if rv.Redacted {
		t.Error("WithRenderShowRedacted: password should not be marked redacted")
	}
	if rv.Value != "secret" {
		t.Errorf("expected raw value %q, got %v", "secret", rv.Value)
	}
}

// --- PrepareRender: RenderConfig (HideEnvVarNames / HideFlagNames) ---

// fieldNamesProvider is a minimal Provider that implements ProviderFieldNamesSupport.
type fieldNamesProvider struct {
	data   kongfig.ConfigData
	names  map[string]string
	source string
}

func (p *fieldNamesProvider) Load(_ context.Context) (kongfig.ConfigData, error) {
	return p.data, nil
}

func (p *fieldNamesProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: p.source}
}
func (p *fieldNamesProvider) FieldNames() map[string]string { return p.names }

func TestPrepareRender_HideEnvVarNames(t *testing.T) {
	ctx := context.Background()

	// Load a provider that registers an env var name for "db.url".
	kf := kongfig.New(kongfig.WithHideEnvVarNames())
	kf.MustLoad(ctx, &fieldNamesProvider{
		data:   kongfig.ConfigData{"db.url": "x"},
		names:  map[string]string{"db.url": "APP_DB_URL"},
		source: "env.test",
	})

	_, renderCtx := kongfig.PrepareRender(ctx, kf)
	// WithHideEnvVarNames on the Kongfig should suppress env var (non-"--") field names.
	if fn := render.FieldNames(renderCtx); fn != nil {
		t.Errorf("expected nil field names when HideEnvVarNames is set, got %v", fn)
	}

	// Without HideEnvVarNames the field names should be present.
	kf2 := kongfig.New()
	kf2.MustLoad(ctx, &fieldNamesProvider{
		data:   kongfig.ConfigData{"db.url": "x"},
		names:  map[string]string{"db.url": "APP_DB_URL"},
		source: "env.test",
	})
	_, renderCtx2 := kongfig.PrepareRender(ctx, kf2)
	if fn := render.FieldNames(renderCtx2); fn == nil {
		t.Error("expected field names to be present when HideEnvVarNames is not set")
	}
}

// --- PrepareRender: filter source ---

type filterTestConfig struct {
	Host string `kongfig:"host"`
}

func TestPrepareRender_FilterSource(t *testing.T) {
	ctx := context.Background()
	kf := kongfig.NewFor[filterTestConfig]()
	kf.MustLoad(ctx, structsprovider.Defaults(filterTestConfig{Host: "localhost"}))

	// Filter to only defaults — result must contain the defaults layer value.
	data, _ := kongfig.PrepareRender(ctx, kf, kongfig.WithRenderFilterSource([]string{"defaults"}))
	rv, ok := data["host"].(kongfig.RenderedValue)
	if !ok {
		t.Fatalf("expected RenderedValue for host, got %T", data["host"])
	}
	if rv.Value != "localhost" {
		t.Errorf("expected defaults value %q, got %v", "localhost", rv.Value)
	}
}

// --- BuildFilterSource ---

func TestBuildFilterSource_AllFalse(t *testing.T) {
	got := render.BuildFilterSource(map[string]bool{"env": false, "defaults": false})
	want := []string{"no-defaults", "no-env"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d: %v", len(got), len(want), got)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestBuildFilterSource_AllTrue(t *testing.T) {
	got := render.BuildFilterSource(map[string]bool{"env": true})
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestBuildFilterSource_Mixed(t *testing.T) {
	got := render.BuildFilterSource(map[string]bool{"env": true, "defaults": false})
	if len(got) != 1 || got[0] != "no-defaults" {
		t.Errorf("expected [no-defaults], got %v", got)
	}
}

// --- MatchesFilterSource ---

func TestMatchesFilterSource_EmptyFilters(t *testing.T) {
	if !render.MatchesFilterSource("env", nil) {
		t.Error("empty filters should match everything")
	}
}

func TestMatchesFilterSource_NoEnv_ExcludesEnvSources(t *testing.T) {
	filters := []string{"no-env"}
	for _, src := range []string{"env", "env.tag", "env.prefix"} {
		if render.MatchesFilterSource(src, filters) {
			t.Errorf("source %q should be excluded by no-env", src)
		}
	}
	if !render.MatchesFilterSource("file", filters) {
		t.Error("file should not be excluded by no-env")
	}
}

func TestMatchesFilterSource_Allowlist(t *testing.T) {
	filters := []string{"env"}
	if !render.MatchesFilterSource("env", filters) {
		t.Error("env should match allowlist env")
	}
	if !render.MatchesFilterSource("env.tag", filters) {
		t.Error("env.tag should match allowlist env")
	}
	if render.MatchesFilterSource("defaults", filters) {
		t.Error("defaults should not match allowlist env")
	}
}

func TestMatchesFilterSource_PrefixMatch(t *testing.T) {
	filters := []string{"file"}
	if !render.MatchesFilterSource("file.xdg.yaml", filters) {
		t.Error("file.xdg.yaml should match allowlist file")
	}
}

func TestMatchesFilterSource_NoFile_ExcludesFileSubSources(t *testing.T) {
	filters := []string{"no-file"}
	if render.MatchesFilterSource("file.xdg.yaml", filters) {
		t.Error("file.xdg.yaml should be excluded by no-file")
	}
}

func TestMatchesFilterSource_ExactMatch(t *testing.T) {
	filters := []string{"defaults"}
	if !render.MatchesFilterSource("defaults", filters) {
		t.Error("defaults should match allowlist defaults exactly")
	}
}

// --- RenderAnnotation via context ---

func TestRenderAnnotation_EnvVarName(t *testing.T) {
	s := &tracingStyler{}
	// Field names in context, but LayerMeta.Data is nil → ProviderData.RenderAnnotation
	// is never called → result is just the styled kind.
	const sid = kongfig.SourceID(1)
	ctx := kongfig.WithRenderFieldNamesCtx(context.Background(), kongfig.PathFieldNames{
		"db.url": {sid: "APP_DB_URL"},
	})
	meta := kongfig.LayerMeta{Kind: kongfig.KindEnv, ID: sid}
	rv := kongfig.RenderedValue{Source: kongfig.SourceMeta{Layer: meta}}

	got := render.Annotation(ctx, rv, "db.url", s)
	// tracingStyler.SourceKind wraps with "[kind:...]"; env var name lookup happens in env.ProviderData,
	// not in LayerMeta when Data is nil — so result is just the styled kind.
	if got != "[kind:env]" {
		t.Errorf("got %q, want %q", got, "[kind:env]")
	}
}

func TestRenderAnnotation_EmptySource(t *testing.T) {
	s := &tracingStyler{}
	rv := kongfig.RenderedValue{} // zero SourceMeta
	got := render.Annotation(context.Background(), rv, "host", s)
	if got != "" {
		t.Errorf("got %q, want empty string for zero source", got)
	}
}

func TestRenderAnnotation_VerboseEnv(t *testing.T) {
	ctx := context.Background()
	verbose := map[string][]string{
		"db.url": {"env.tag", "env.kong"},
	}
	_, renderCtx := kongfig.PrepareRender(ctx, kongfig.New(), kongfig.WithRenderVerboseSources(verbose))
	s := &tracingStyler{}
	meta := kongfig.LayerMeta{Kind: kongfig.KindEnv, Name: "env.tag"}
	rv := kongfig.RenderedValue{Source: kongfig.SourceMeta{Layer: meta}}

	got := render.Annotation(renderCtx, rv, "db.url", s)
	// verbose env with two sub-sources → "[env.tag, env.kong]" passed to SourceKind
	want := "[kind:[env.tag, env.kong]]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
