package json_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
)

func TestRoundTrip(t *testing.T) {
	original := map[string]any{
		"host": "localhost",
		"port": float64(8080),
		"db": map[string]any{
			"name": "mydb",
		},
	}

	b, err := jsonparser.Default.Marshal(original)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	got, err := jsonparser.Default.Unmarshal(b)
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
	got, err := jsonparser.Default.Unmarshal([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestUnmarshalNull(t *testing.T) {
	input := `{"key": null, "other": "value"}`
	got, err := jsonparser.Default.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if got["key"] != nil {
		t.Errorf("null key: expected nil, got %v", got["key"])
	}
	if got["other"] != "value" {
		t.Errorf("other: got %v", got["other"])
	}
}

func TestBindRender(t *testing.T) {
	data := map[string]any{"host": "localhost", "port": float64(8080)}
	var buf bytes.Buffer

	r := jsonparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "host") {
		t.Errorf("expected 'host' in output, got: %s", out)
	}
	if !strings.Contains(out, `"localhost"`) {
		t.Errorf("expected quoted string value, got: %s", out)
	}
}

// --- stripComments via WithComments.Unmarshal ---

func TestStripComments_LineComment(t *testing.T) {
	input := `{
	"host": "localhost", // this is a comment
	"port": 8080
}`
	got, err := jsonparser.WithComments.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}
	if got["host"] != "localhost" {
		t.Errorf("host: got %v", got["host"])
	}
	if got["port"] != float64(8080) {
		t.Errorf("port: got %v", got["port"])
	}
}

func TestStripComments_BlockComment(t *testing.T) {
	input := `{
	/* block comment before key */
	"host": "localhost"
}`
	got, err := jsonparser.WithComments.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}
	if got["host"] != "localhost" {
		t.Errorf("host: got %v", got["host"])
	}
}

func TestStripComments_InlineBlockComment(t *testing.T) {
	input := `{"host": /* inline */ "localhost"}`
	got, err := jsonparser.WithComments.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}
	if got["host"] != "localhost" {
		t.Errorf("host: got %v", got["host"])
	}
}

func TestStripComments_CommentInsideString_NotStripped(t *testing.T) {
	// "http://example.com" contains // which must NOT be treated as a comment.
	input := `{"url": "http://example.com"}`
	got, err := jsonparser.WithComments.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}
	if got["url"] != "http://example.com" {
		t.Errorf("url: got %v", got["url"])
	}
}

func TestStripComments_BlockCommentInsideString_NotStripped(t *testing.T) {
	input := `{"msg": "hello /* world */"}`
	got, err := jsonparser.WithComments.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}
	if got["msg"] != "hello /* world */" {
		t.Errorf("msg: got %v", got["msg"])
	}
}

func TestStripComments_EscapedQuoteAdjacentToSlash(t *testing.T) {
	// String contains \", the parser must not exit the string context early.
	input := `{"q": "say \"hi\" // not a comment"}`
	got, err := jsonparser.WithComments.Unmarshal([]byte(input))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}
	want := `say "hi" // not a comment`
	if got["q"] != want {
		t.Errorf("q: got %v, want %v", got["q"], want)
	}
}

func TestStripComments_UnclosedBlockComment(t *testing.T) {
	// An unclosed block comment should consume the rest of the input. The
	// resulting JSON is invalid; Unmarshal should return an error (not panic).
	input := `{"host": "localhost" /* unclosed`
	// After stripping, output is `{"host": "localhost" ` which is invalid JSON.
	_, err := jsonparser.WithComments.Unmarshal([]byte(input))
	if err == nil {
		t.Error("expected error for unclosed block comment, got nil")
	}
}

// --- Compact variant ---

func TestCompact_RenderNoIndent(t *testing.T) {
	data := kongfig.ConfigData{"key": "val"}
	var buf bytes.Buffer

	r := jsonparser.Compact.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Compact output must not contain indented newlines between key and value.
	if strings.Contains(out, "\n  ") {
		t.Errorf("compact output should not contain indented lines, got: %s", out)
	}
}

func TestCompact_Marshal(t *testing.T) {
	data := kongfig.ConfigData{"a": "b"}
	b, err := jsonparser.Compact.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	// Should be single-line JSON (no newlines except the trailing one).
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("compact marshal should produce one line, got %d: %s", len(lines), b)
	}
}

// --- RenderNoComments context key ---

func TestRenderNoComments_SuppressesAnnotation(t *testing.T) {
	// Build a RenderedValue with a non-zero SourceMeta so an annotation would
	// normally be emitted when Comments=true.
	meta := kongfig.LayerMeta{Kind: kongfig.KindDefaults, Name: "defaults"}
	rv := kongfig.RenderedValue{
		Value:  "localhost",
		Source: kongfig.SourceMeta{Layer: meta},
	}
	data := kongfig.ConfigData{"host": rv}

	// Without noComments: annotation should appear (JSONC mode).
	var buf bytes.Buffer
	r := jsonparser.WithComments.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	withAnnotation := buf.String()

	// With noComments: annotation must be absent.
	buf.Reset()
	ctx := kongfig.WithRenderNoCommentsCtx(context.Background())
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	without := buf.String()

	// The annotation is the source kind "defaults"; when noComments it should be gone.
	if strings.Contains(without, "defaults") {
		t.Errorf("noComments: annotation 'defaults' should be suppressed, got: %s", without)
	}
	// Sanity: without noComments it IS present.
	if !strings.Contains(withAnnotation, "defaults") {
		t.Errorf("with comments: annotation 'defaults' should be present, got: %s", withAnnotation)
	}
}

// --- RenderedValue wrapping ---

func TestRenderedValue_UnwrappedCorrectly(t *testing.T) {
	rv := kongfig.RenderedValue{Value: "hello"}
	data := kongfig.ConfigData{"key": rv}

	var buf bytes.Buffer
	r := jsonparser.Default.Bind(plainStyler{})
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
	r := jsonparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "***") {
		t.Errorf("expected redacted display in output, got: %s", out)
	}
}

// --- RenderHelpTexts ---

func TestRenderHelpTexts_InjectsComments(t *testing.T) {
	data := kongfig.ConfigData{"host": "localhost", "port": float64(8080)}
	helps := map[string]string{"host": "the server hostname"}
	ctx := kongfig.WithRenderHelpTextsCtx(context.Background(), helps)

	var buf bytes.Buffer
	r := jsonparser.WithComments.Bind(plainStyler{})
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "// the server hostname") {
		t.Errorf("expected help comment in output, got:\n%s", out)
	}
}

// --- RenderAlignSources ---

func TestRenderAlignSources_AlignedOutput(t *testing.T) {
	meta := kongfig.LayerMeta{Kind: kongfig.KindDefaults, Name: "defaults"}
	rv1 := kongfig.RenderedValue{Value: "localhost", Source: kongfig.SourceMeta{Layer: meta}}
	rv2 := kongfig.RenderedValue{Value: float64(8080), Source: kongfig.SourceMeta{Layer: meta}}
	data := kongfig.ConfigData{"host": rv1, "port": rv2}

	ctx := context.Background()
	var buf bytes.Buffer
	r := jsonparser.WithComments.Bind(plainStyler{})
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Both annotations should be present and columns aligned.
	if strings.Count(out, "// defaults") != 2 {
		t.Errorf("expected 2 annotations, got:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var annotated []string
	for _, l := range lines {
		if strings.Contains(l, "// defaults") {
			annotated = append(annotated, l)
		}
	}
	if len(annotated) == 2 {
		idx0 := strings.Index(annotated[0], "// defaults")
		idx1 := strings.Index(annotated[1], "// defaults")
		if idx0 != idx1 {
			t.Errorf("annotations not aligned: col %d vs %d\n%s\n%s", idx0, idx1, annotated[0], annotated[1])
		}
	}
}

// --- Empty ConfigData ---

func TestBindRender_EmptyData(t *testing.T) {
	var buf bytes.Buffer
	r := jsonparser.Default.Bind(plainStyler{})
	if err := r.Render(context.Background(), &buf, kongfig.ConfigData{}); err != nil {
		t.Fatal("render of empty ConfigData should not error:", err)
	}
}

// --- Styler dispatch ---

func TestStylerDispatch(t *testing.T) {
	s := &trackingStyler{}
	data := kongfig.ConfigData{
		"count":  float64(42),
		"flag":   true,
		"name":   "alice",
		"absent": nil,
	}
	r := jsonparser.Default.Bind(s)
	if err := r.Render(context.Background(), &buf{}, data); err != nil {
		t.Fatal(err)
	}
	if !s.numberCalled {
		t.Error("Number() was not called for float64 value")
	}
	if !s.boolCalled {
		t.Error("Bool() was not called for bool value")
	}
	if !s.nullCalled {
		t.Error("Null() was not called for nil value")
	}
}

// buf is a discard writer for tracking styler tests.
type buf struct{ bytes.Buffer }

// plainStyler is a no-op Styler for tests.
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
