package env_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	envprovider "github.com/pmarschik/kongfig/providers/env"
)

func TestProviderPrefix(t *testing.T) {
	t.Setenv("APP_HOST", "myhost")
	t.Setenv("APP_DB_NAME", "mydb")
	t.Setenv("OTHER_VAR", "ignored")

	p := envprovider.Provider("APP", "_")
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if data["host"] != "myhost" {
		t.Errorf("host: got %v", data["host"])
	}
	db, ok := data["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db not a map: %T", data["db"])
	}
	if db["name"] != "mydb" {
		t.Errorf("db.name: got %v", db["name"])
	}
	if _, ok := data["other"]; ok {
		t.Error("OTHER_VAR should be excluded")
	}
}

func TestProviderWithCallback(t *testing.T) {
	t.Setenv("APP_LOG_LEVEL", "debug")

	p := envprovider.ProviderWithCallback("APP_", func(key string) string {
		return strings.ToLower(strings.ReplaceAll(key, "_", "-"))
	})
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["log-level"] != "debug" {
		t.Errorf("log-level: got %v", data["log-level"])
	}
}

func TestBindRender(t *testing.T) {
	p := envprovider.Provider("APP", "_")
	r := p.Bind(plainStyler{})

	data := map[string]any{"host": "localhost", "port": "8080"}
	var buf bytes.Buffer
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "export") {
		t.Errorf("expected 'export' in output, got: %s", out)
	}
	if !strings.Contains(out, "HOST") {
		t.Errorf("expected 'HOST' in output, got: %s", out)
	}
}

func TestSourceDataRenderAnnotation_WithVarName(t *testing.T) {
	s := recordingStyler{}
	const sid = kongfig.SourceID(42)
	ctx := kongfig.WithRenderFieldNamesCtx(context.Background(), kongfig.PathFieldNames{
		"db.url": {sid: "APP_DB_URL"},
	})
	ctx = kongfig.WithSourceIDCtx(ctx, sid)

	got := envprovider.ProviderData{}.RenderAnnotation(ctx, &s, "db.url")

	if got != "$APP_DB_URL" {
		t.Errorf("got %q, want %q", got, "$APP_DB_URL")
	}
	if len(s.sourceKeyCalls) != 1 || s.sourceKeyCalls[0] != "$APP_DB_URL" {
		t.Errorf("SourceKey calls: %v", s.sourceKeyCalls)
	}
}

func TestSourceDataRenderAnnotation_NoVarName(t *testing.T) {
	s := recordingStyler{}
	got := envprovider.ProviderData{}.RenderAnnotation(context.Background(), &s, "db.url")

	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
	if len(s.sourceKeyCalls) != 0 {
		t.Errorf("SourceKey should not be called: %v", s.sourceKeyCalls)
	}
}

func TestSourceDataRenderAnnotation_HiddenVarNames(t *testing.T) {
	s := recordingStyler{}
	// WithHideEnvVarNames via RenderConfig — simulate by passing a context
	// where fieldNames is nil (no entries registered).
	ctx := kongfig.WithRenderFieldNamesCtx(context.Background(), nil)

	got := envprovider.ProviderData{}.RenderAnnotation(ctx, &s, "db.url")

	if got != "" {
		t.Errorf("got %q, want empty string when var names hidden", got)
	}
}

func TestLayerMetaRenderAnnotation_WithVarName(t *testing.T) {
	s := recordingStyler{}
	const sid = kongfig.SourceID(7)
	ctx := kongfig.WithRenderFieldNamesCtx(context.Background(), kongfig.PathFieldNames{
		"host": {sid: "APP_HOST"},
	})

	meta := kongfig.LayerMeta{Kind: kongfig.KindEnv, Data: envprovider.ProviderData{}, ID: sid}
	got := meta.RenderAnnotation(ctx, &s, "host")

	// LayerMeta renders "kind (data)" where data is the pre-styled SourceKey output
	if got != "env ($APP_HOST)" {
		t.Errorf("got %q, want %q", got, "env ($APP_HOST)")
	}
}

func TestLayerMetaRenderAnnotation_NoVarName(t *testing.T) {
	s := recordingStyler{}
	meta := kongfig.LayerMeta{Kind: kongfig.KindEnv, Data: envprovider.ProviderData{}}
	got := meta.RenderAnnotation(context.Background(), &s, "host")

	// No var name → empty data → just the kind, no parens
	if got != "env" {
		t.Errorf("got %q, want %q", got, "env")
	}
}

// recordingStyler records SourceKey calls; all other methods are pass-through.
type recordingStyler struct {
	sourceKeyCalls []string
}

func (*recordingStyler) Key(v string) string           { return v }
func (*recordingStyler) String(v string) string        { return v }
func (*recordingStyler) Number(v string) string        { return v }
func (*recordingStyler) Bool(v string) string          { return v }
func (*recordingStyler) Null(v string) string          { return v }
func (*recordingStyler) Syntax(v string) string        { return v }
func (*recordingStyler) Comment(v string) string       { return v }
func (*recordingStyler) Annotation(_, v string) string { return v }
func (*recordingStyler) SourceKind(v string) string    { return v }
func (*recordingStyler) SourceData(v string) string    { return v }
func (s *recordingStyler) SourceKey(v string) string {
	s.sourceKeyCalls = append(s.sourceKeyCalls, v)
	return v
}
func (*recordingStyler) Redacted(v string) string { return v }
func (*recordingStyler) Codec(v string) string    { return v }

func TestProviderWithKeyFunc_BasicMapping(t *testing.T) {
	t.Setenv("MY_APP_HOST", "testhost")
	t.Setenv("MY_APP_DB_PORT", "5432")
	t.Setenv("UNRELATED_VAR", "ignored")

	p := envprovider.ProviderWithKeyFunc(func(key string) string {
		switch key {
		case "MY_APP_HOST":
			return "host"
		case "MY_APP_DB_PORT":
			return "db.port"
		default:
			return ""
		}
	})
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["host"] != "testhost" {
		t.Errorf("host: got %v", data["host"])
	}
	db, ok := data["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db not a map: %T", data["db"])
	}
	if db["port"] != "5432" {
		t.Errorf("db.port: got %v", db["port"])
	}
	if _, ok := data["unrelated"]; ok {
		t.Error("UNRELATED_VAR should be excluded")
	}
}

func TestProviderWithKeyFunc_ProviderInfo(t *testing.T) {
	p := envprovider.ProviderWithKeyFunc(func(string) string { return "" })
	if p.ProviderInfo().Name != "env.keyfunc" {
		t.Errorf("ProviderInfo().Name: got %q, want %q", p.ProviderInfo().Name, "env.keyfunc")
	}
}

func TestProviderWithKeyFunc_FieldNames(t *testing.T) {
	t.Setenv("MY_APP_HOST", "testhost")

	p := envprovider.ProviderWithKeyFunc(func(key string) string {
		if key == "MY_APP_HOST" {
			return "host"
		}
		return ""
	})
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	names := p.FieldNames()
	if names == nil {
		t.Fatal("FieldNames: got nil")
	}
	if names["host"] != "MY_APP_HOST" {
		t.Errorf("FieldNames[host]: got %q, want %q", names["host"], "MY_APP_HOST")
	}
}

func TestProviderWithKeyFunc_SkipsEmpty(t *testing.T) {
	t.Setenv("KEEP_ME", "yes")
	t.Setenv("SKIP_ME", "no")

	p := envprovider.ProviderWithKeyFunc(func(key string) string {
		if key == "KEEP_ME" {
			return "keep"
		}
		return ""
	})
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data["keep"] != "yes" {
		t.Errorf("keep: got %v", data["keep"])
	}
	if _, ok := data["skip"]; ok {
		t.Error("SKIP_ME should be excluded")
	}
}

func TestLoaderProviderData(t *testing.T) {
	p := envprovider.Provider("APP", "_")
	if p.ProviderData() == nil {
		t.Fatal("expected non-nil ProviderData")
	}
	if p.ProviderInfo().Name != "env.prefix" {
		t.Errorf("ProviderInfo().Name: got %q, want %q", p.ProviderInfo().Name, "env.prefix")
	}
}

func TestProviderDeepNesting(t *testing.T) {
	t.Setenv("APP_DB_PRIMARY_HOST", "primary.db.local")
	t.Setenv("APP_DB_PRIMARY_PORT", "5432")
	t.Setenv("APP_DB_REPLICA_HOST", "replica.db.local")

	p := envprovider.Provider("APP", "_")
	data, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	db, ok := data["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db not a map: %T", data["db"])
	}
	primary, ok := db["primary"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db.primary not a map: %T", db["primary"])
	}
	if primary["host"] != "primary.db.local" {
		t.Errorf("db.primary.host: got %v", primary["host"])
	}
	if primary["port"] != "5432" {
		t.Errorf("db.primary.port: got %v", primary["port"])
	}

	replica, ok := db["replica"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db.replica not a map: %T", db["replica"])
	}
	if replica["host"] != "replica.db.local" {
		t.Errorf("db.replica.host: got %v", replica["host"])
	}

	// Also verify FieldNames records the full dot path.
	names := p.FieldNames()
	if names["db.primary.host"] != "APP_DB_PRIMARY_HOST" {
		t.Errorf("FieldNames[db.primary.host]: got %q", names["db.primary.host"])
	}
}

func TestProviderConcurrentFieldNames(t *testing.T) {
	t.Setenv("CONCTEST_HOST", "host1")
	t.Setenv("CONCTEST_PORT", "8080")

	p := envprovider.Provider("CONCTEST", "_")
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	const goroutines = 20
	errs := make(chan string, goroutines)

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			names := p.FieldNames()
			if names == nil {
				errs <- "FieldNames returned nil"
				return
			}
			if names["host"] != "CONCTEST_HOST" {
				errs <- "FieldNames[host] mismatch"
			}
		})
	}
	wg.Wait()
	close(errs)

	for msg := range errs {
		t.Error(msg)
	}
}

type plainStyler struct{}

func (plainStyler) Key(s string) string           { return s }
func (plainStyler) String(s string) string        { return s }
func (plainStyler) Number(s string) string        { return s }
func (plainStyler) Bool(s string) string          { return s }
func (plainStyler) Null(s string) string          { return s }
func (plainStyler) Syntax(s string) string        { return s }
func (plainStyler) Comment(s string) string       { return s }
func (plainStyler) Annotation(_, s string) string { return s }
func (plainStyler) SourceKind(s string) string    { return s }
func (plainStyler) SourceData(s string) string    { return s }
func (plainStyler) SourceKey(s string) string     { return s }
func (plainStyler) Redacted(s string) string      { return s }
func (plainStyler) Codec(s string) string         { return s }
