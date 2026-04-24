package kongfig_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
)

// captureHandler is an slog.Handler that captures all records for assertions.
type captureHandler struct {
	records []slog.Record
	mu      sync.Mutex
}

func (*captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

// staticProvider is a test Provider.
type staticProvider struct {
	data   map[string]any
	source string
}

func (p *staticProvider) Load(_ context.Context) (kongfig.ConfigData, error) {
	return kongfig.ConfigData(p.data), nil
}

func (p *staticProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: p.source}
}

func mustLoad(t *testing.T, k *kongfig.Kongfig, p kongfig.Provider) {
	t.Helper()
	if err := k.Load(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMerge(t *testing.T) {
	k := kongfig.New()

	if err := k.Load(context.Background(), &staticProvider{data: map[string]any{"host": "localhost", "port": 8080}, source: "defaults"}); err != nil {
		t.Fatal(err)
	}
	if err := k.Load(context.Background(), &staticProvider{data: map[string]any{"host": "prod.example.com"}, source: "file"}); err != nil {
		t.Fatal(err)
	}

	raw := k.All()
	if raw["host"] != "prod.example.com" {
		t.Errorf("host: got %v, want prod.example.com", raw["host"])
	}
	if raw["port"] != 8080 {
		t.Errorf("port: got %v, want 8080", raw["port"])
	}
}

func TestProvenance(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "defaults"})
	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "prod.example.com"}, source: "file"})

	prov := k.Provenance()
	metas := prov.SourceMetas()
	if metas["host"].Layer.Name != "file" {
		t.Errorf("provenance host: got %q, want %q", metas["host"].Layer.Name, "file")
	}
}

func TestAll(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"db": map[string]any{"host": "localhost", "port": 5432}}, source: "defaults"})

	all := k.All()
	db, ok := all["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("all[db]: want ConfigData, got %T", all["db"])
	}
	if db["host"] != "localhost" {
		t.Errorf("db.host: got %v, want localhost", db["host"])
	}
	if db["port"] != 5432 {
		t.Errorf("db.port: got %v (%T), want 5432 (int)", db["port"], db["port"])
	}
}

func TestFlat(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"db": map[string]any{"host": "localhost", "port": 5432}}, source: "defaults"})

	flat := k.Flat()
	if flat["db.host"] != "localhost" {
		t.Errorf("db.host: got %v, want localhost", flat["db.host"])
	}
	if flat["db.port"] != 5432 {
		t.Errorf("db.port: got %v (%T), want 5432 (int)", flat["db.port"], flat["db.port"])
	}
}

func TestExists(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"a": map[string]any{"b": "val"}}, source: "test"})

	if !k.Exists("a.b") {
		t.Error("expected a.b to exist")
	}
	if k.Exists("a.c") {
		t.Error("expected a.c to not exist")
	}
}

func TestCut(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"db": map[string]any{"host": "localhost"}}, source: "test"})

	sub := k.Cut("db")
	raw := sub.All()
	if raw["host"] != "localhost" {
		t.Errorf("cut host: got %v, want localhost", raw["host"])
	}
}

// TestCutProvenance verifies that Cut() correctly maps provenance into the child Kongfig.
func TestCutProvenance(t *testing.T) {
	t.Run("child_key_has_provenance", func(t *testing.T) {
		k := kongfig.New()
		mustLoad(t, k, &staticProvider{
			data:   map[string]any{"db": map[string]any{"host": "localhost"}},
			source: "defaults",
		})

		child := k.Cut("db")
		sm, ok := child.SourceFor("host")
		if !ok {
			t.Fatal("SourceFor(host) on Cut child: expected provenance, got none")
		}
		if sm.Layer.Name != "defaults" {
			t.Errorf("SourceFor(host).Layer.Name = %q, want %q", sm.Layer.Name, "defaults")
		}
	})

	t.Run("cut_point_scalar_maps_to_root", func(t *testing.T) {
		k := kongfig.New()
		mustLoad(t, k, &staticProvider{
			data:   map[string]any{"db": "scalar-value"},
			source: "file",
		})

		child := k.Cut("db")
		sm, ok := child.SourceFor("")
		if !ok {
			t.Fatal("SourceFor(\"\") on Cut child of scalar path: expected provenance, got none")
		}
		if sm.Layer.Name != "file" {
			t.Errorf("SourceFor(\"\").Layer.Name = %q, want %q", sm.Layer.Name, "file")
		}
	})
}

func TestOnLoad(t *testing.T) {
	k := kongfig.New()
	var fired []string
	k.OnLoad(func(e kongfig.LoadEvent) kongfig.LoadResult {
		fired = append(fired, e.Layer.Meta.Name)
		return kongfig.LoadResult{}
	})

	mustLoad(t, k, &staticProvider{data: map[string]any{"x": 1}, source: "s1"})
	mustLoad(t, k, &staticProvider{data: map[string]any{"y": 2}, source: "s2"})

	if len(fired) != 2 || fired[0] != "s1" || fired[1] != "s2" {
		t.Errorf("onLoad fired: %v", fired)
	}
}

func TestWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	k := kongfig.New()
	changed := make(chan struct{}, 1)
	k.OnChange(func() { changed <- struct{}{} })

	wp := &fakeWatcher{ch: make(chan kongfig.ConfigData, 1)}
	k.AddWatcher(wp)

	go func() {
		//nolint:errcheck // Watch returns ctx.Err on cancel; ignored in test
		_ = k.Watch(ctx)
	}()

	wp.ch <- kongfig.ConfigData{"live": "yes"}

	select {
	case <-changed:
		// good
	case <-ctx.Done():
		t.Error("timeout waiting for OnChange")
	}
}

type fakeWatcher struct {
	ch chan kongfig.ConfigData
}

func (*fakeWatcher) Load(_ context.Context) (kongfig.ConfigData, error) { return nil, nil }
func (*fakeWatcher) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: "watch.fake"}
}

func (w *fakeWatcher) Watch(ctx context.Context, cb kongfig.WatchFunc) error {
	for {
		select {
		case data := <-w.ch:
			cb(kongfig.WatchDataEvent{Data: data})
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// envTagProvider is a staticProvider that returns "env.tag" as its source.
type envTagProvider struct {
	data map[string]any
}

func (p *envTagProvider) Load(_ context.Context) (kongfig.ConfigData, error) {
	return kongfig.ConfigData(p.data), nil
}

func (*envTagProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: "env.tag", Kind: kongfig.KindEnv}
}

// envPrefixProvider is a staticProvider that returns "env.prefix" as its source.
type envPrefixProvider struct {
	data map[string]any
}

func (p *envPrefixProvider) Load(_ context.Context) (kongfig.ConfigData, error) {
	return kongfig.ConfigData(p.data), nil
}

func (*envPrefixProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: "env.prefix", Kind: kongfig.KindEnv}
}

func TestWarnEnvCollisions_SameValue_NoWarn(t *testing.T) {
	h := &captureHandler{}
	k := kongfig.New(kongfig.WithLogger(slog.New(h)))

	mustLoad(t, k, &envTagProvider{data: map[string]any{"db": map[string]any{"url": "lolz"}}})
	mustLoad(t, k, &envPrefixProvider{data: map[string]any{"db": map[string]any{"url": "lolz"}}})

	h.mu.Lock()
	n := len(h.records)
	h.mu.Unlock()
	if n != 0 {
		t.Errorf("expected no warnings for same value, got %d record(s)", n)
	}
}

func TestWarnEnvCollisions_DifferentValue_Warns(t *testing.T) {
	h := &captureHandler{}
	k := kongfig.New(kongfig.WithLogger(slog.New(h)))

	mustLoad(t, k, &envTagProvider{data: map[string]any{"db": map[string]any{"url": "lolz"}}})
	mustLoad(t, k, &envPrefixProvider{data: map[string]any{"db": map[string]any{"url": "different"}}})

	h.mu.Lock()
	records := h.records
	h.mu.Unlock()

	if len(records) == 0 {
		t.Fatal("expected at least one warning for differing values, got none")
	}
	found := false
	for _, r := range records {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "key" && a.Value.String() == "db.url" {
				found = true
				return false
			}
			return true
		})
		if found {
			break
		}
	}
	if !found {
		t.Errorf("expected warning with key=db.url, records: %v", records)
	}
}

// TestLayersIsolation verifies that mutating a Layer.Data map returned by Layers()
// does not affect Kongfig's internal state.
func TestLayersIsolation(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "test"})

	layers := k.Layers()
	if len(layers) != 1 {
		t.Fatalf("want 1 layer, got %d", len(layers))
	}
	// Mutate the returned snapshot.
	layers[0].Data["host"] = "mutated"
	layers[0].Data["extra"] = "injected"

	// Original state must be unchanged.
	layers2 := k.Layers()
	if got := layers2[0].Data["host"]; got != "localhost" {
		t.Errorf("Layers() isolation: host = %q, want %q", got, "localhost")
	}
	if _, ok := layers2[0].Data["extra"]; ok {
		t.Error("Layers() isolation: injected key leaked into internal state")
	}
}

// captureParserNamer is a Parser+ParserNamer+OutputProvider that captures the
// ConfigData passed to its renderer, for inspecting RenderedValue metadata.
type captureParserNamer struct {
	captured kongfig.ConfigData
}

func (*captureParserNamer) Unmarshal(_ []byte) (kongfig.ConfigData, error) { return nil, nil }
func (*captureParserNamer) Marshal(_ kongfig.ConfigData) ([]byte, error)   { return nil, nil }
func (*captureParserNamer) Format() string                                 { return "capture" }
func (*captureParserNamer) Extensions() []string                           { return nil }
func (p *captureParserNamer) Bind(_ kongfig.Styler) kongfig.Renderer       { return &captureRenderer{p: p} }

type captureRenderer struct{ p *captureParserNamer }

func (r *captureRenderer) Render(_ context.Context, _ io.Writer, data kongfig.ConfigData) error {
	r.p.captured = data
	return nil
}

// TestRedactionBehavior verifies that paths registered with WithRedacted are
// marked Redacted in the RenderedValue passed to renderers.
func TestRedactionBehavior(t *testing.T) {
	k := kongfig.New(kongfig.WithRedacted(map[string]bool{"secret": true}))
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"secret": "hunter2", "plain": "visible"},
		source: "test",
	})

	cp := &captureParserNamer{}
	k.RegisterParsers(cp)

	if err := k.Render(context.Background(), io.Discard, render.BaseStyler{}); err != nil {
		t.Fatal(err)
	}

	rv, ok := cp.captured["secret"].(kongfig.RenderedValue)
	if !ok {
		t.Fatalf("expected RenderedValue for 'secret', got %T", cp.captured["secret"])
	}
	if !rv.Redacted {
		t.Error("expected 'secret' to be marked Redacted")
	}

	rvPlain, ok := cp.captured["plain"].(kongfig.RenderedValue)
	if !ok {
		t.Fatalf("expected RenderedValue for 'plain', got %T", cp.captured["plain"])
	}
	if rvPlain.Redacted {
		t.Error("expected 'plain' to NOT be marked Redacted")
	}
}

// --- helpers for Render + LoadParsed tests ---

// fakeParserNamer is a minimal Parser+ParserNamer+OutputProvider for testing.
type fakeParserNamer struct {
	format string
	output string // written verbatim by the renderer
}

func (*fakeParserNamer) Unmarshal(_ []byte) (kongfig.ConfigData, error) { return nil, nil }
func (*fakeParserNamer) Marshal(_ kongfig.ConfigData) ([]byte, error)   { return nil, nil }
func (p *fakeParserNamer) Format() string                               { return p.format }
func (*fakeParserNamer) Extensions() []string                           { return nil }
func (p *fakeParserNamer) Bind(_ kongfig.Styler) kongfig.Renderer {
	return &fakeRenderer{out: p.output}
}

type fakeRenderer struct{ out string }

func (r *fakeRenderer) Render(_ context.Context, w io.Writer, _ kongfig.ConfigData) error {
	_, err := io.WriteString(w, r.out)
	return err
}

// TestLoadParsedWithParser verifies that LoadParsed with WithParser records the
// parser on the layer and registers it in the Kongfig's parser list.
func TestLoadParsedWithParser(t *testing.T) {
	p := &fakeParserNamer{format: "yaml", output: ""}
	k := kongfig.New()
	if err := k.LoadParsed(kongfig.ConfigData{"host": "localhost"}, "file", kongfig.WithParser(p)); err != nil {
		t.Fatal(err)
	}

	layers := k.Layers()
	if len(layers) != 1 {
		t.Fatalf("want 1 layer, got %d", len(layers))
	}
	if layers[0].Parser != p {
		t.Error("LoadParsed: WithParser did not attach parser to layer")
	}

	parsers := k.Parsers()
	found := false
	for _, rp := range parsers {
		if rp == p {
			found = true
		}
	}
	if !found {
		t.Error("LoadParsed: WithParser did not register parser in Kongfig")
	}
}

// TestLoadParsedWithProviderData verifies that LoadParsed with WithProviderData
// stamps Data onto the resulting LayerMeta (Kind still inferred from source name).
func TestLoadParsedWithProviderData(t *testing.T) {
	k := kongfig.New()
	if err := k.LoadParsed(kongfig.ConfigData{"host": "localhost"}, "file", kongfig.WithProviderData((*fakeProviderData)(nil))); err != nil {
		t.Fatal(err)
	}

	layers := k.Layers()
	if layers[0].Meta.Kind != kongfig.KindFile {
		t.Errorf("Layer.Meta.Kind = %q, want %q", layers[0].Meta.Kind, kongfig.KindFile)
	}
	if layers[0].Meta.Name != "file" {
		t.Errorf("Layer.Meta.Name = %q, want %q", layers[0].Meta.Name, "file")
	}
}

// fakeProviderData is a no-op ProviderData for tests.
type fakeProviderData struct{}

func (*fakeProviderData) RenderAnnotation(_ context.Context, _ kongfig.Styler, _ string) string {
	return ""
}

// TestLoadParsedLayerMeta verifies that LoadParsed stamps Name, Kind (inferred from
// source prefix), and a unique ID onto Layer.Meta.
func TestLoadParsedLayerMeta(t *testing.T) {
	k := kongfig.New()
	if err := k.LoadParsed(kongfig.ConfigData{"host": "localhost"}, "env.tag"); err != nil {
		t.Fatal(err)
	}
	layers := k.Layers()
	if len(layers) != 1 {
		t.Fatalf("want 1 layer, got %d", len(layers))
	}
	if layers[0].Meta.Name != "env.tag" {
		t.Errorf("Layer.Meta.Name = %q, want %q", layers[0].Meta.Name, "env.tag")
	}
	if layers[0].Meta.Kind != kongfig.KindEnv {
		t.Errorf("Layer.Meta.Kind = %q, want %q", layers[0].Meta.Kind, kongfig.KindEnv)
	}
	if layers[0].Meta.ID == 0 {
		t.Error("Layer.Meta.ID should be non-zero")
	}
}

// TestRenderFormatSelection verifies that WithRenderFormat picks the correct parser
// when multiple renderable parsers are registered, making format selection
// independent of registration order.
func TestRenderFormatSelection(t *testing.T) {
	yaml := &fakeParserNamer{format: "yaml", output: "yaml-output"}
	toml := &fakeParserNamer{format: "toml", output: "toml-output"}

	k := kongfig.New(kongfig.WithParsers(yaml, toml))

	var buf bytes.Buffer
	if err := k.Render(context.Background(), &buf, render.BaseStyler{}, kongfig.WithRenderFormat("toml")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "toml-output" {
		t.Errorf("Render with Format=toml: got %q, want %q", got, "toml-output")
	}

	buf.Reset()
	if err := k.Render(context.Background(), &buf, render.BaseStyler{}, kongfig.WithRenderFormat("yaml")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "yaml-output" {
		t.Errorf("Render with Format=yaml: got %q, want %q", got, "yaml-output")
	}
}

// TestRenderErrNoRenderer verifies that Render returns ErrNoRenderer when
// no matching parser is registered.
func TestRenderErrNoRenderer(t *testing.T) {
	k := kongfig.New()
	err := k.Render(context.Background(), io.Discard, render.BaseStyler{})
	if !errors.Is(err, kongfig.ErrNoRenderer) {
		t.Errorf("expected ErrNoRenderer, got %v", err)
	}

	yaml := &fakeParserNamer{format: "yaml", output: "yaml-output"}
	k2 := kongfig.New(kongfig.WithParsers(yaml))
	err = k2.Render(context.Background(), io.Discard, render.BaseStyler{}, kongfig.WithRenderFormat("toml"))
	if !errors.Is(err, kongfig.ErrNoRenderer) {
		t.Errorf("expected ErrNoRenderer for unknown format, got %v", err)
	}
}

// TestRenderDefaultFormatFirstRegistered verifies that when opts.Format is empty
// and no WithDefaultFormat is set, Render uses the first registered OutputProvider
// (deterministic: follows Load/WithParsers order).
func TestRenderDefaultFormatFirstRegistered(t *testing.T) {
	yaml := &fakeParserNamer{format: "yaml", output: "yaml-output"}
	toml := &fakeParserNamer{format: "toml", output: "toml-output"}

	// yaml registered first
	k := kongfig.New(kongfig.WithParsers(yaml, toml))

	var buf bytes.Buffer
	if err := k.Render(context.Background(), &buf, render.BaseStyler{}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "yaml-output" {
		t.Errorf("default format: got %q, want %q (first registered)", got, "yaml-output")
	}
}

// TestRenderWithDefaultFormat verifies that WithDefaultFormat pins the instance-level
// default format, overriding registration order but still allowing per-call opts.Format.
func TestRenderWithDefaultFormat(t *testing.T) {
	yaml := &fakeParserNamer{format: "yaml", output: "yaml-output"}
	toml := &fakeParserNamer{format: "toml", output: "toml-output"}

	// yaml registered first, but default pinned to toml
	k := kongfig.New(
		kongfig.WithParsers(yaml, toml),
		kongfig.WithDefaultFormat("toml"),
	)

	var buf bytes.Buffer
	// No explicit Format: should use the pinned default (toml)
	if err := k.Render(context.Background(), &buf, render.BaseStyler{}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "toml-output" {
		t.Errorf("default format: got %q, want %q (WithDefaultFormat)", got, "toml-output")
	}

	// Explicit per-call Format still overrides the pinned default
	buf.Reset()
	if err := k.Render(context.Background(), &buf, render.BaseStyler{}, kongfig.WithRenderFormat("yaml")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "yaml-output" {
		t.Errorf("explicit format: got %q, want %q", got, "yaml-output")
	}
}
