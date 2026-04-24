package toml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

func TestRoundTrip(t *testing.T) {
	original := map[string]any{
		"host": "localhost",
		"port": int64(8080),
		"db": map[string]any{
			"name": "mydb",
		},
	}

	b, err := tomlparser.Default.Marshal(original)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	got, err := tomlparser.Default.Unmarshal(b)
	if err != nil {
		t.Fatal("unmarshal:", err)
	}

	if got["host"] != "localhost" {
		t.Errorf("host: got %v", got["host"])
	}
	db, ok := got["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db not a map, got %T", got["db"])
	}
	if db["name"] != "mydb" {
		t.Errorf("db.name: got %v", db["name"])
	}
}

func TestUnmarshalEmpty(t *testing.T) {
	got, err := tomlparser.Default.Unmarshal([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestUnmarshalNested(t *testing.T) {
	input := `
[server]
host = "prod.example.com"
port = 9000

[server.tls]
enabled = true
cert = "/etc/cert.pem"
`
	got, err := tomlparser.Default.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	server, ok := got["server"].(kongfig.ConfigData)
	if !ok {
		t.Fatal("server not a map")
	}
	if server["host"] != "prod.example.com" {
		t.Errorf("server.host: got %v", server["host"])
	}
	tls, ok := server["tls"].(kongfig.ConfigData)
	if !ok {
		t.Fatal("server.tls not a map")
	}
	if tls["enabled"] != true {
		t.Errorf("server.tls.enabled: got %v", tls["enabled"])
	}
}

func TestBindRender(t *testing.T) {
	data := map[string]any{"host": "localhost", "port": int64(8080)}
	var buf bytes.Buffer

	r := tomlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "host") {
		t.Errorf("expected 'host' in output, got: %s", out)
	}
	if !strings.Contains(out, `"localhost"`) {
		t.Errorf("expected quoted string value in output, got: %s", out)
	}
}

// --- Nested tables ---

func TestBindRender_NestedTable(t *testing.T) {
	data := kongfig.ConfigData{
		"host": "localhost",
		"db": kongfig.ConfigData{
			"name": "mydb",
			"port": int64(5432),
		},
	}
	var buf bytes.Buffer
	r := tomlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Nested ConfigData must render as a [section] header.
	if !strings.Contains(out, "[db]") {
		t.Errorf("expected '[db]' section header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "name") {
		t.Errorf("expected 'name' key under [db], got:\n%s", out)
	}
}

func TestBindRender_DeeplyNestedTable(t *testing.T) {
	data := kongfig.ConfigData{
		"server": kongfig.ConfigData{
			"tls": kongfig.ConfigData{
				"enabled": true,
			},
		},
	}
	var buf bytes.Buffer
	r := tomlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// TOML dotted section header for nested map.
	if !strings.Contains(out, "server") {
		t.Errorf("expected 'server' section in output, got:\n%s", out)
	}
	if !strings.Contains(out, "enabled") {
		t.Errorf("expected 'enabled' key in output, got:\n%s", out)
	}
}

// --- RenderedValue wrapping ---

func TestRenderedValue_UnwrappedCorrectly(t *testing.T) {
	rv := kongfig.RenderedValue{Value: "hello"}
	data := kongfig.ConfigData{"key": rv}

	var buf bytes.Buffer
	r := tomlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"hello"`) {
		t.Errorf("expected unwrapped string value in output, got: %s", out)
	}
}

func TestRenderedValue_Redacted(t *testing.T) {
	rv := kongfig.RenderedValue{Redacted: true, RedactedDisplay: "***"}
	data := kongfig.ConfigData{"secret": rv}

	var buf bytes.Buffer
	r := tomlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "***") {
		t.Errorf("expected redacted display in output, got: %s", out)
	}
}

func TestRenderedValue_NestedTable(t *testing.T) {
	// A RenderedValue wrapping a ConfigData sub-map must render as a table section,
	// not as a scalar.
	rv := kongfig.RenderedValue{Value: kongfig.ConfigData{"name": "mydb"}}
	data := kongfig.ConfigData{"db": rv}

	var buf bytes.Buffer
	r := tomlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "[db]") {
		t.Errorf("expected '[db]' section header for RenderedValue wrapping ConfigData, got:\n%s", out)
	}
}

// --- Empty ConfigData ---

func TestBindRender_EmptyData(t *testing.T) {
	var buf bytes.Buffer
	r := tomlparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, kongfig.ConfigData{}); err != nil {
		t.Fatal("render of empty ConfigData should not error:", err)
	}
}

// --- RenderAlignSources ---

func TestRenderAlignSources_AlignedOutput(t *testing.T) {
	meta := kongfig.LayerMeta{Kind: kongfig.KindDefaults, Name: "defaults"}
	src := kongfig.SourceMeta{Layer: meta}
	data := kongfig.ConfigData{
		"a_short":         kongfig.RenderedValue{Value: "x", Source: src},
		"a_very_long_key": kongfig.RenderedValue{Value: "y", Source: src},
	}

	ctx := context.Background()
	var buf bytes.Buffer
	r := tomlparser.Default.Bind(plainStyler{})
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "defaults") {
		t.Fatalf("expected annotation in output, got:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var annotated []string
	for _, l := range lines {
		if strings.Contains(l, "# defaults") {
			annotated = append(annotated, l)
		}
	}
	if len(annotated) == 2 {
		idx0 := strings.Index(annotated[0], "# defaults")
		idx1 := strings.Index(annotated[1], "# defaults")
		if idx0 != idx1 {
			t.Errorf("annotations not aligned: col %d vs %d\n%s\n%s", idx0, idx1, annotated[0], annotated[1])
		}
	}
}

// --- Styler dispatch ---

func TestStylerDispatch(t *testing.T) {
	s := &trackingStyler{}
	data := kongfig.ConfigData{
		"count": int64(42),
		"flag":  true,
		"name":  "alice",
	}
	var buf bytes.Buffer
	r := tomlparser.Default.Bind(s)
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	if !s.numberCalled {
		t.Error("Number() was not called for int64 value")
	}
	if !s.boolCalled {
		t.Error("Bool() was not called for bool value")
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
