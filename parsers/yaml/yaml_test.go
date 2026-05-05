package yaml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
)

func TestRoundTrip(t *testing.T) {
	original := map[string]any{
		"host": "localhost",
		"port": 8080,
		"db": map[string]any{
			"name": "mydb",
		},
	}

	b, err := yamlparser.Default.Marshal(original)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	got, err := yamlparser.Default.Unmarshal(b)
	if err != nil {
		t.Fatal("unmarshal:", err)
	}

	if got["host"] != "localhost" {
		t.Errorf("host: got %v", got["host"])
	}
	db, ok := got["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatal("db not a map")
	}
	if db["name"] != "mydb" {
		t.Errorf("db.name: got %v", db["name"])
	}
}

func TestUnmarshalEmpty(t *testing.T) {
	got, err := yamlparser.Default.Unmarshal([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestBindRender(t *testing.T) {
	data := map[string]any{"host": "localhost", "port": 8080}
	var buf bytes.Buffer

	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("host")) {
		t.Errorf("expected 'host' in output, got: %s", out)
	}
}

// --- Null values ---

func TestUnmarshalNullKeyword(t *testing.T) {
	input := "key: null\nother: value\n"
	got, err := yamlparser.Default.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if got["key"] != nil {
		t.Errorf("null keyword: expected nil, got %v", got["key"])
	}
	if got["other"] != "value" {
		t.Errorf("other: got %v", got["other"])
	}
}

func TestUnmarshalNullTilde(t *testing.T) {
	input := "key: ~\nother: value\n"
	got, err := yamlparser.Default.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if got["key"] != nil {
		t.Errorf("tilde null: expected nil, got %v", got["key"])
	}
}

// --- YAML alignment two-pass rendering ---

func TestRenderAlignSources_AlignedOutput(t *testing.T) {
	// Build two RenderedValues with source annotations so the renderer has
	// something to align. Use a source with a kind so RenderAnnotation returns
	// a non-empty string.
	meta := kongfig.LayerMeta{Kind: kongfig.KindDefaults, Name: "defaults"}
	src := kongfig.SourceMeta{Layer: meta}
	data := kongfig.ConfigData{
		"a_short":         kongfig.RenderedValue{Value: "x", Source: src},
		"a_very_long_key": kongfig.RenderedValue{Value: "y", Source: src},
	}

	var buf bytes.Buffer
	ctx := context.Background()
	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Each annotated line should contain the annotation; alignment means both
	// annotations start at the same column. Verify both keys and annotations appear.
	if !strings.Contains(out, "a_short") {
		t.Errorf("expected 'a_short' in output, got: %s", out)
	}
	if !strings.Contains(out, "a_very_long_key") {
		t.Errorf("expected 'a_very_long_key' in output, got: %s", out)
	}
	if !strings.Contains(out, "defaults") {
		t.Errorf("expected source annotation 'defaults' in output, got: %s", out)
	}
	// Alignment: find the column of the annotation on each annotated line.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var annCols []int
	for _, line := range lines {
		idx := strings.Index(line, "# ")
		if idx >= 0 {
			annCols = append(annCols, idx)
		}
	}
	if len(annCols) < 2 {
		t.Fatalf("expected at least 2 annotated lines, got %d; output:\n%s", len(annCols), out)
	}
	for i := 1; i < len(annCols); i++ {
		if annCols[i] != annCols[0] {
			t.Errorf("annotations not aligned: line 0 col %d, line %d col %d; output:\n%s",
				annCols[0], i, annCols[i], out)
		}
	}
}

// --- RenderedValue wrapping ---

func TestRenderedValue_UnwrappedCorrectly(t *testing.T) {
	rv := kongfig.RenderedValue{Value: "hello"}
	data := kongfig.ConfigData{"key": rv}

	var buf bytes.Buffer
	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("expected unwrapped value in output, got: %s", out)
	}
}

func TestRenderedValue_Redacted(t *testing.T) {
	rv := kongfig.RenderedValue{Redacted: true, RedactedDisplay: "***"}
	data := kongfig.ConfigData{"secret": rv}

	var buf bytes.Buffer
	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "***") {
		t.Errorf("expected redacted display in output, got: %s", out)
	}
}

// --- Empty ConfigData ---

func TestBindRender_EmptyData(t *testing.T) {
	var b bytes.Buffer
	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &b, kongfig.ConfigData{}); err != nil {
		t.Fatal("render of empty ConfigData should not error:", err)
	}
}

// --- Styler dispatch ---

func TestStylerDispatch(t *testing.T) {
	s := &trackingStyler{}
	data := kongfig.ConfigData{
		"count": 42,
		"flag":  true,
		"name":  "alice",
		"empty": nil,
	}
	var b bytes.Buffer
	r := yamlparser.Default.Bind(s)
	if err := r.Render(context.Background(), &b, data); err != nil {
		t.Fatal(err)
	}
	if !s.numberCalled {
		t.Error("Number() was not called for int value")
	}
	if !s.boolCalled {
		t.Error("Bool() was not called for bool value")
	}
	if !s.nullCalled {
		t.Error("Null() was not called for nil value")
	}
}

// plainStyler is a local no-op Styler for tests.
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

// trackingStyler records which typed Styler methods were called.
type trackingStyler struct {
	numberCalled bool
	boolCalled   bool
	nullCalled   bool
}

func (*trackingStyler) Key(s string) string           { return s }
func (*trackingStyler) String(s string) string        { return s }
func (t *trackingStyler) Number(s string) string      { t.numberCalled = true; return s }
func (t *trackingStyler) Bool(s string) string        { t.boolCalled = true; return s }
func (t *trackingStyler) Null(s string) string        { t.nullCalled = true; return s }
func (*trackingStyler) Syntax(s string) string        { return s }
func (*trackingStyler) Comment(s string) string       { return s }
func (*trackingStyler) Annotation(_, s string) string { return s }
func (*trackingStyler) SourceKind(s string) string    { return s }
func (*trackingStyler) SourceData(s string) string    { return s }
func (*trackingStyler) SourceKey(s string) string     { return s }
func (*trackingStyler) Redacted(s string) string      { return s }
func (*trackingStyler) Codec(s string) string         { return s }

// staticProvider is a minimal Provider for use in tests.
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

// taggedItem is a generic named struct for testing typed-slice rendering.
type taggedItem struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value,omitempty"`
}

func TestBindRender_TypedSlice(t *testing.T) {
	// []taggedItem is not []any or []string — must still render as YAML flow list.
	items := []taggedItem{{Name: "alpha", Value: "one"}, {Name: "beta"}}
	data := kongfig.ConfigData{"items": kongfig.RenderedValue{Value: items}}
	var buf bytes.Buffer
	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "map[") {
		t.Errorf("got Go %%v format for typed slice — expected YAML flow syntax:\n%s", out)
	}
	if !strings.Contains(out, "items: [") {
		t.Errorf("expected items rendered as YAML flow list, got:\n%s", out)
	}
}

func TestBindRender_AnySliceOfMaps(t *testing.T) {
	// []any containing ConfigData elements (the post-unmarshal form for YAML lists of maps).
	items := []any{
		kongfig.ConfigData{"name": "alpha", "value": "one"},
		kongfig.ConfigData{"name": "beta"},
	}
	data := kongfig.ConfigData{"items": kongfig.RenderedValue{Value: items}}
	var buf bytes.Buffer
	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "map[") {
		t.Errorf("got Go %%v format for []any slice — expected YAML flow syntax:\n%s", out)
	}
	if !strings.Contains(out, "items: [") {
		t.Errorf("expected items rendered as YAML flow list, got:\n%s", out)
	}
}

func TestBindRender_BlockCollections(t *testing.T) {
	// WithRenderBlockCollections forces block style regardless of terminal width.
	items := []string{"alpha", "beta", "gamma"}
	data := kongfig.ConfigData{"items": kongfig.RenderedValue{Value: items}}
	ctx := kongfig.WithRenderBlockCollectionsCtx(context.Background())
	var buf bytes.Buffer
	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "items: [") {
		t.Errorf("expected block style, got inline:\n%s", out)
	}
	if !strings.Contains(out, "- alpha") {
		t.Errorf("expected YAML block list with '- alpha', got:\n%s", out)
	}
}

// markerStyler wraps Key calls with "<<" / ">>" so tests can verify styling was applied.
type markerStyler struct{ plainStyler }

func (markerStyler) Key(s string) string { return "<<" + s + ">>" }

func TestBindRender_BlockSliceOfMaps_KeysStyled(t *testing.T) {
	// Slice-of-maps in block mode: keys inside each map element must go through s.Key().
	items := []any{
		kongfig.ConfigData{"dir": "path/a", "parent": "scope/b"},
	}
	data := kongfig.ConfigData{"aux": items}
	ctx := kongfig.WithRenderBlockCollectionsCtx(context.Background())

	s := markerStyler{}
	var buf bytes.Buffer
	r := yamlparser.Default.Bind(s)
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "<<dir>>") {
		t.Errorf("expected s.Key() applied to 'dir' in block output, got:\n%s", out)
	}
	if !strings.Contains(out, "<<parent>>") {
		t.Errorf("expected s.Key() applied to 'parent' in block output, got:\n%s", out)
	}
	if !strings.Contains(out, "- <<dir>>") {
		t.Errorf("expected '- <<dir>>' sequence marker in output, got:\n%s", out)
	}
}

func TestBindRender_BlockSliceOfMaps_Structure(t *testing.T) {
	// Verify the block YAML structure for a slice of maps is valid.
	items := []any{
		kongfig.ConfigData{"dir": "ixopay-research/foo", "parent": "iron-cactus-squad/bar"},
		kongfig.ConfigData{"dir": "other/path"},
	}
	data := kongfig.ConfigData{"aux": items}
	ctx := kongfig.WithRenderBlockCollectionsCtx(context.Background())

	var buf bytes.Buffer
	r := yamlparser.Default.Bind(plainStyler{})
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "- dir:") && !strings.Contains(out, "- dir: ") {
		t.Errorf("expected '- dir:' sequence entry in block output, got:\n%s", out)
	}
	if !strings.Contains(out, "parent:") {
		t.Errorf("expected 'parent' key in block output, got:\n%s", out)
	}
}

func TestYAMLRenderer_AlignSources(t *testing.T) {
	kf := kongfig.New()
	if err := kf.Load(context.Background(), &staticProvider{
		data:   map[string]any{"host": "localhost", "log-level": "info", "port": 8080},
		source: "defaults",
	}); err != nil {
		t.Fatal(err)
	}

	r := yamlparser.Default.Bind(plainStyler{})
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
		t.Skipf("fewer than 2 annotated lines, skipping:\n%s", out)
	}
	for i, c := range cols[1:] {
		if c != cols[0] {
			t.Errorf("annotation column mismatch: line0=%d, line%d=%d\n%s", cols[0], i+1, c, out)
		}
	}
}
