package kongfig_test

// core_api_test.go — coverage for core API methods that lacked tests.
//
// read_when: you are adding tests for root-package kongfig APIs.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
)

// ── Validate ─────────────────────────────────────────────────────────────────

func TestValidate_NilValidator(t *testing.T) {
	k := kongfig.New()
	if err := k.Validate(); err != nil {
		t.Errorf("Validate with no validator: got %v, want nil", err)
	}
}

func TestValidate_ValidatorPropagatesError(t *testing.T) {
	want := errors.New("invalid config")
	k := kongfig.New(kongfig.WithValidator(&errorValidator{err: want}))
	got := k.Validate()
	if !errors.Is(got, want) {
		t.Errorf("Validate: got %v, want %v", got, want)
	}
}

func TestValidate_ValidatorCalledAfterLoad(t *testing.T) {
	var sawValue any
	v := &captureValidator{
		fn: func(k *kongfig.Kongfig) error {
			sawValue = k.All()["key"]
			return nil
		},
	}
	k := kongfig.New(kongfig.WithValidator(v))
	mustLoad(t, k, &staticProvider{data: map[string]any{"key": "loaded"}, source: "test"})
	if err := k.Validate(); err != nil {
		t.Fatal(err)
	}
	if sawValue != "loaded" {
		t.Errorf("validator saw %v, want %q", sawValue, "loaded")
	}
}

// errorValidator is a ConfigValidator that always returns a fixed error.
type errorValidator struct{ err error }

func (v *errorValidator) ValidateConfig(_ *kongfig.Kongfig) error { return v.err }

// captureValidator is a ConfigValidator that calls fn.
type captureValidator struct {
	fn func(*kongfig.Kongfig) error
}

func (v *captureValidator) ValidateConfig(k *kongfig.Kongfig) error { return v.fn(k) }

// ── FieldNames ────────────────────────────────────────────────────────────────

func TestFieldNames_NilWhenNoProviders(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "test"})
	if fn := k.FieldNames(); fn != nil {
		t.Errorf("FieldNames with no ProviderFieldNamesSupport: got %v, want nil", fn)
	}
}

func TestFieldNames_RegisteredByFieldNamesProvider(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &fieldNamesProvider{
		data:   kongfig.ConfigData{"db.host": "localhost"},
		names:  map[string]string{"db.host": "APP_DB_HOST"},
		source: "env.test",
	})
	fn := k.FieldNames()
	if fn == nil {
		t.Fatal("FieldNames: expected non-nil map after loading ProviderFieldNamesSupport provider")
	}
	// The map is keyed path → SourceID → field name; we just need at least one entry for "db.host".
	if len(fn["db.host"]) == 0 {
		t.Errorf("FieldNames: expected entry for db.host, got %v", fn)
	}
	for _, name := range fn["db.host"] {
		if name != "APP_DB_HOST" {
			t.Errorf("FieldNames[db.host][sid] = %q, want APP_DB_HOST", name)
		}
	}
}

func TestFieldNames_IsolationFromMutation(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &fieldNamesProvider{
		data:   kongfig.ConfigData{"host": "x"},
		names:  map[string]string{"host": "APP_HOST"},
		source: "env.test",
	})
	fn1 := k.FieldNames()
	for sid := range fn1["host"] {
		fn1["host"][sid] = "MUTATED"
	}
	fn2 := k.FieldNames()
	for _, name := range fn2["host"] {
		if name == "MUTATED" {
			t.Error("FieldNames() isolation: mutation of returned map affected internal state")
		}
	}
}

// ── SetMergeFunc ──────────────────────────────────────────────────────────────

func TestSetMergeFunc_AppendSlice(t *testing.T) {
	k := kongfig.New()
	// Register an append-merge for the "tags" key.
	k.SetMergeFunc("tags", func(dst, src any) (any, error) {
		var dSlice []any
		if s, ok := dst.([]any); ok {
			dSlice = s
		}
		var sSlice []any
		if s, ok := src.([]any); ok {
			sSlice = s
		}
		return append(dSlice, sSlice...), nil
	})

	mustLoad(t, k, &staticProvider{data: map[string]any{"tags": []any{"a", "b"}}, source: "defaults"})
	mustLoad(t, k, &staticProvider{data: map[string]any{"tags": []any{"c"}}, source: "override"})

	raw := k.All()
	tags, ok := raw["tags"].([]any)
	if !ok {
		t.Fatalf("tags: got %T, want []any", raw["tags"])
	}
	if len(tags) != 3 {
		t.Errorf("tags after append-merge: got %v, want [a b c]", tags)
	}
}

func TestSetMergeFunc_LastWriteWinsOnError(t *testing.T) {
	k := kongfig.New()
	// Merge func that always errors → falls back to last-write-wins.
	k.SetMergeFunc("key", func(_, _ any) (any, error) {
		return nil, errors.New("force LWW fallback")
	})

	mustLoad(t, k, &staticProvider{data: map[string]any{"key": "first"}, source: "s1"})
	mustLoad(t, k, &staticProvider{data: map[string]any{"key": "second"}, source: "s2"})

	if got := k.All()["key"]; got != "second" {
		t.Errorf("last-write-wins fallback: got %v, want second", got)
	}
}

// ── RegisterCodec ─────────────────────────────────────────────────────────────

func TestRegisterCodec_BidirectionalAppliesAtLoad(t *testing.T) {
	k := kongfig.New()
	// Bidirectional codec (has Encode): Decode runs at load time.
	k.RegisterCodec("host", kongfig.Of(kongfig.Codec[string]{
		Decode: func(v any) (string, error) {
			if s, ok := v.(string); ok {
				return "DECODED:" + s, nil
			}
			return "", errors.New("not a string")
		},
		Encode: func(v string) string { return v },
	}))

	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "test"})

	// Bidirectional codecs apply at load time: raw value is replaced in the store.
	if got := k.All()["host"]; got != "DECODED:localhost" {
		t.Errorf("RegisterCodec bidirectional decode: got %v, want DECODED:localhost", got)
	}
}

func TestRegisterCodec_DecodeOnlyAppliesAtGet(t *testing.T) {
	type cfg struct {
		Host string `kongfig:"host"`
	}
	k := kongfig.New()
	// Decode-only codec (no Encode): raw value is preserved in store; Decode runs at Get time.
	k.RegisterCodec("host", kongfig.DecodeOnly(func(v any) any {
		if s, ok := v.(string); ok {
			return "DECODED:" + s
		}
		return v
	}))

	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "test"})

	// Raw value is preserved in the store.
	if got := k.All()["host"]; got != "localhost" {
		t.Errorf("DecodeOnly store: got %v, want raw localhost", got)
	}
	// Decoded value appears via Get.
	out, err := kongfig.Get[cfg](k)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out.Host != "DECODED:localhost" {
		t.Errorf("DecodeOnly Get: got %v, want DECODED:localhost", out.Host)
	}
}

func TestRegisterCodec_EncodeAppliedOnRender(t *testing.T) {
	// Register a parser that implements OutputProvider so we can call Render.
	p := &recordingParser{}
	k := kongfig.New(kongfig.WithParsers(p))
	k.RegisterCodec("host", kongfig.Of(kongfig.Codec[string]{
		Decode: func(v any) (string, error) {
			if s, ok := v.(string); ok {
				return s, nil
			}
			return "", nil
		},
		Encode: func(v string) string { return "ENCODED:" + v },
	}))

	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "test"})

	// After PrepareRender, the RenderedValue for "host" should have Encoded=true and
	// Value = "ENCODED:localhost".
	data, _ := kongfig.PrepareRender(context.Background(), k)
	rv, ok := data["host"].(kongfig.RenderedValue)
	if !ok {
		t.Fatalf("host: expected RenderedValue, got %T", data["host"])
	}
	if !rv.Encoded {
		t.Error("RegisterCodec Encode: Encoded flag not set on RenderedValue")
	}
	if rv.Value != "ENCODED:localhost" {
		t.Errorf("RegisterCodec Encode: Value = %v, want ENCODED:localhost", rv.Value)
	}
}

// recordingParser is a minimal Parser+ParserNamer+OutputProvider for use in render tests.
type recordingParser struct {
	rendered []kongfig.ConfigData
}

func (*recordingParser) Unmarshal(_ []byte) (kongfig.ConfigData, error) { return nil, nil }
func (*recordingParser) Marshal(_ kongfig.ConfigData) ([]byte, error)   { return nil, nil }
func (*recordingParser) Format() string                                 { return "record" }
func (*recordingParser) Extensions() []string                           { return []string{".rec"} }
func (p *recordingParser) Bind(_ kongfig.Styler) kongfig.Renderer {
	return &recordingRenderer{p: p}
}

type recordingRenderer struct{ p *recordingParser }

func (r *recordingRenderer) Render(_ context.Context, w io.Writer, data kongfig.ConfigData) error {
	r.p.rendered = append(r.p.rendered, data)
	_, err := io.WriteString(w, "ok")
	return err
}

// ── ParserForPath ─────────────────────────────────────────────────────────────

func TestParserForPath_MatchesByExtension(t *testing.T) {
	yaml := &fakeParserNamer{format: "yaml", output: ""}
	// fakeParserNamer uses nil Extensions(); we need an actual extension-aware one.
	p := &extParser{format: "yaml", exts: []string{".yaml", ".yml"}}
	parser, err := kongfig.ParserForPath("config.yaml", []kongfig.Parser{p, yaml})
	if err != nil {
		t.Fatalf("ParserForPath: got error %v", err)
	}
	if parser != p {
		t.Error("ParserForPath: returned wrong parser")
	}
}

func TestParserForPath_ReturnsErrorForUnknownExtension(t *testing.T) {
	p := &extParser{format: "yaml", exts: []string{".yaml"}}
	_, err := kongfig.ParserForPath("config.toml", []kongfig.Parser{p})
	if err == nil {
		t.Error("ParserForPath: expected error for unknown extension, got nil")
	}
}

func TestParserForPath_NilParsersReturnsError(t *testing.T) {
	_, err := kongfig.ParserForPath("config.yaml", nil)
	if err == nil {
		t.Error("ParserForPath: expected error for nil parsers slice, got nil")
	}
}

// extParser is a Parser+ParserNamer with configurable extensions.
type extParser struct {
	format string
	exts   []string
}

func (*extParser) Unmarshal(_ []byte) (kongfig.ConfigData, error) { return nil, nil }
func (*extParser) Marshal(_ kongfig.ConfigData) ([]byte, error)   { return nil, nil }
func (p *extParser) Format() string                               { return p.format }
func (p *extParser) Extensions() []string                         { return p.exts }

// ── RenderLayers ──────────────────────────────────────────────────────────────

func TestRenderLayers_CallbackCalledPerLayer(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"a": 1}, source: "s1"})
	mustLoad(t, k, &staticProvider{data: map[string]any{"b": 2}, source: "s2"})

	var names []string
	err := k.RenderLayers(context.Background(), func(_ context.Context, layer kongfig.Layer, _ kongfig.ConfigData) error {
		names = append(names, layer.Meta.Name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "s1" || names[1] != "s2" {
		t.Errorf("RenderLayers: got layers %v, want [s1 s2]", names)
	}
}

func TestRenderLayers_WithFilterSourceFiltersLayers(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"a": 1}, source: "defaults"})
	mustLoad(t, k, &envTagProvider{data: map[string]any{"b": 2}}) // source: "env.tag"

	var names []string
	err := k.RenderLayers(context.Background(), func(_ context.Context, layer kongfig.Layer, _ kongfig.ConfigData) error {
		names = append(names, layer.Meta.Name)
		return nil
	}, kongfig.WithRenderFilterSource([]string{"defaults"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "defaults" {
		t.Errorf("RenderLayers + FilterSource: got %v, want [defaults]", names)
	}
}

func TestRenderLayers_GroupEnvLayers(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &envTagProvider{data: map[string]any{"a": 1}})    // env.tag
	mustLoad(t, k, &envPrefixProvider{data: map[string]any{"b": 2}}) // env.prefix

	var names []string
	err := k.RenderLayers(context.Background(), func(_ context.Context, layer kongfig.Layer, _ kongfig.ConfigData) error {
		names = append(names, layer.Meta.Name)
		return nil
	}, kongfig.WithRenderGroupEnvLayers())
	if err != nil {
		t.Fatal(err)
	}
	// Two env.* layers should be merged into a single "env" layer.
	if len(names) != 1 || names[0] != "env" {
		t.Errorf("RenderLayers + GroupEnvLayers: got %v, want [env]", names)
	}
}

func TestRenderLayers_CallbackErrorPropagated(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"x": 1}, source: "test"})

	want := errors.New("layer error")
	err := k.RenderLayers(context.Background(), func(_ context.Context, _ kongfig.Layer, _ kongfig.ConfigData) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("RenderLayers: error propagation: got %v, want %v", err, want)
	}
}

// ── Bind ──────────────────────────────────────────────────────────────────────

func TestBind_WithOutputProvider(t *testing.T) {
	p := &fakeParserNamer{format: "yaml", output: "bound-output"}
	renderer := kongfig.Bind(p, render.BaseStyler{})
	if renderer == nil {
		t.Fatal("Bind with OutputProvider: got nil renderer")
	}
	var buf bytes.Buffer
	if err := renderer.Render(context.Background(), &buf, nil); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "bound-output" {
		t.Errorf("Bind OutputProvider render: got %q, want bound-output", got)
	}
}

func TestBind_WithoutOutputProvider_FallsBackToPassthrough(t *testing.T) {
	// plainParser implements Parser but NOT OutputProvider.
	p := &plainParser{}
	renderer := kongfig.Bind(p, render.BaseStyler{})
	if renderer == nil {
		t.Fatal("Bind without OutputProvider: got nil renderer")
	}
	// Render should call Marshal and write its output.
	var buf bytes.Buffer
	if err := renderer.Render(context.Background(), &buf, kongfig.ConfigData{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("Bind passthrough render produced empty output")
	}
}

// plainParser is a Parser with no OutputProvider — triggers the passthrough path.
type plainParser struct{}

func (*plainParser) Unmarshal(_ []byte) (kongfig.ConfigData, error) { return nil, nil }
func (*plainParser) Marshal(_ kongfig.ConfigData) ([]byte, error)   { return []byte("plain-out"), nil }

// ── Watch ─────────────────────────────────────────────────────────────────────

func TestWatch_OnChangeCalledOnReload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	k := kongfig.New()
	changed := make(chan struct{}, 1)
	k.OnChange(func() { changed <- struct{}{} })

	wp := &fakeWatcher{ch: make(chan kongfig.ConfigData, 1)}
	k.AddWatcher(wp)

	go func() {
		//nolint:errcheck // canceled at test end
		_ = k.Watch(ctx)
	}()

	wp.ch <- kongfig.ConfigData{"live": "reload"}

	select {
	case <-changed:
		// OnChange fired — confirm data was merged.
		if got := k.All()["live"]; got != "reload" {
			t.Errorf("Watch reload data: got %v, want reload", got)
		}
	case <-ctx.Done():
		t.Error("timeout: OnChange was not called after Watch reload")
	}
}

func TestWatch_MultipleOnChangeCallbacks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	k := kongfig.New()
	calls := make(chan string, 4)
	k.OnChange(func() { calls <- "first" })
	k.OnChange(func() { calls <- "second" })

	wp := &fakeWatcher{ch: make(chan kongfig.ConfigData, 1)}
	k.AddWatcher(wp)

	go func() {
		//nolint:errcheck // canceled at test end
		_ = k.Watch(ctx)
	}()

	wp.ch <- kongfig.ConfigData{"x": 1}

	got := map[string]bool{}
	for range 2 {
		select {
		case name := <-calls:
			got[name] = true
		case <-ctx.Done():
			t.Error("timeout waiting for OnChange callbacks")
			return
		}
	}
	if !got["first"] || !got["second"] {
		t.Errorf("Watch: not all OnChange callbacks were called: %v", got)
	}
}

// ── Exists (additional edge cases beyond kongfig_test.go) ────────────────────

func TestExists_FalseOnEmptyKongfig(t *testing.T) {
	k := kongfig.New()
	if k.Exists("any.path") {
		t.Error("Exists on empty Kongfig: expected false")
	}
}

func TestExists_TrueAfterLoadingKey(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"host": "localhost"}, source: "test"})
	if !k.Exists("host") {
		t.Error("Exists: expected true for loaded key")
	}
}

func TestExists_FalseForMissingNestedPath(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"db": map[string]any{"host": "localhost"}}, source: "test"})
	if k.Exists("db.missing") {
		t.Error("Exists: expected false for missing nested path")
	}
}

func TestExists_TrueForExistingNestedPath(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"db": map[string]any{"host": "localhost"}}, source: "test"})
	if !k.Exists("db.host") {
		t.Error("Exists: expected true for existing nested path")
	}
}

// ── Cut (additional edge cases beyond kongfig_test.go) ───────────────────────

func TestCut_ProvenancePreserved(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"db": map[string]any{"host": "localhost", "port": 5432}},
		source: "defaults",
	})
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"db": map[string]any{"host": "prod.example.com"}},
		source: "file",
	})

	child := k.Cut("db")

	sm, ok := child.SourceFor("host")
	if !ok {
		t.Fatal("Cut: SourceFor(host) on child: expected provenance, got none")
	}
	if sm.Layer.Name != "file" {
		t.Errorf("Cut provenance: host source = %q, want file", sm.Layer.Name)
	}

	sm2, ok := child.SourceFor("port")
	if !ok {
		t.Fatal("Cut: SourceFor(port) on child: expected provenance, got none")
	}
	if sm2.Layer.Name != "defaults" {
		t.Errorf("Cut provenance: port source = %q, want defaults", sm2.Layer.Name)
	}
}

func TestCut_AllReturnsOnlySubtree(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{
		data:   map[string]any{"db": map[string]any{"host": "localhost"}, "other": "value"},
		source: "test",
	})

	child := k.Cut("db")
	all := child.All()

	if _, ok := all["other"]; ok {
		t.Error("Cut().All(): unexpected key 'other' from sibling tree")
	}
	if all["host"] != "localhost" {
		t.Errorf("Cut().All(): host = %v, want localhost", all["host"])
	}
}

func TestCut_NonexistentPathReturnsEmpty(t *testing.T) {
	k := kongfig.New()
	mustLoad(t, k, &staticProvider{data: map[string]any{"x": 1}, source: "test"})

	child := k.Cut("nonexistent")
	if len(child.All()) != 0 {
		t.Errorf("Cut(nonexistent).All(): expected empty, got %v", child.All())
	}
}
