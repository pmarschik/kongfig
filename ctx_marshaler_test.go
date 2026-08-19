package kongfig_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	"github.com/pmarschik/kongfig/render"
)

// ctxMarshalParser is a Parser that is not an OutputProvider, so kongfig.Bind
// wraps it in the passthrough renderer. It records which of the two marshal
// methods was called and what the ctx-aware one was told about key order.
type ctxMarshalParser struct {
	order    []string
	sawCtx   bool
	sawPlain bool
}

func (*ctxMarshalParser) Unmarshal([]byte) (kongfig.ConfigData, error) {
	return kongfig.ConfigData{}, nil
}

func (p *ctxMarshalParser) Marshal(kongfig.ConfigData) ([]byte, error) {
	p.sawPlain = true
	return []byte("plain\n"), nil
}

func (p *ctxMarshalParser) MarshalCtx(ctx context.Context, _ kongfig.ConfigData) ([]byte, error) {
	p.sawCtx = true
	p.order = render.KeyOrder(ctx, "")
	return []byte("ctx\n"), nil
}

// A parser that can marshal with a context gets one: the passthrough renderer
// holds the render ctx, and dropping it costs the parser every option the
// render call established.
func TestPassthrough_PrefersMarshalCtx(t *testing.T) {
	p := &ctxMarshalParser{}
	kf := kongfig.New()
	loadTOML(t, kf, "file", "zebra = 1\napple = 2\n")

	var buf bytes.Buffer
	if err := kf.RenderWith(context.Background(), &buf, kongfig.Bind(p, mockStyler{})); err != nil {
		t.Fatal("render:", err)
	}

	if p.sawPlain || !p.sawCtx {
		t.Fatalf("MarshalCtx called = %v, Marshal called = %v; want the ctx-aware one",
			p.sawCtx, p.sawPlain)
	}
	if buf.String() != "ctx\n" {
		t.Errorf("output = %q, want the bytes MarshalCtx returned", buf.String())
	}
	if strings.Join(p.order, ",") != "zebra,apple" {
		t.Errorf("key order seen = %v, want the document's [zebra apple]", p.order)
	}
}

// plainMarshalParser marshals without a context. The passthrough renderer must still
// work for it — CtxMarshaler is an optional interface, not a new requirement.
type plainMarshalParser struct{ called bool }

func (*plainMarshalParser) Unmarshal([]byte) (kongfig.ConfigData, error) {
	return kongfig.ConfigData{}, nil
}

func (p *plainMarshalParser) Marshal(kongfig.ConfigData) ([]byte, error) {
	p.called = true
	return []byte("plain\n"), nil
}

func TestPassthrough_FallsBackToMarshal(t *testing.T) {
	p := &plainMarshalParser{}
	var buf bytes.Buffer
	if err := kongfig.New().RenderWith(
		context.Background(), &buf, kongfig.Bind(p, mockStyler{}),
	); err != nil {
		t.Fatal("render:", err)
	}
	if !p.called || buf.String() != "plain\n" {
		t.Errorf("Marshal called = %v, output = %q; want the plain path", p.called, buf.String())
	}
}

// The helper is the supported way to hand a key order to a marshal call, so a
// caller building a ctx by hand does not have to reach for the raw option key.
func TestWithRenderKeyOrderCtx_IsReadBack(t *testing.T) {
	ctx := kongfig.WithRenderKeyOrderCtx(context.Background(),
		map[string][]string{"": {"zebra", "apple"}})

	if got := render.KeyOrder(ctx, ""); strings.Join(got, ",") != "zebra,apple" {
		t.Errorf("render.KeyOrder = %v, want [zebra apple]", got)
	}
}

// Marshal round-trips the document: what the parser reported on the way in is
// what the emitter writes on the way out, so a rewritten config file does not
// come back alphabetized.
func TestTOMLMarshalCtx_RoundTripsDocumentOrder(t *testing.T) {
	const src = "zebra = 1\napple = 2\n\n[server]\nport = 8080\nhost = \"h\"\n"

	data, order, err := tomlparser.Default.UnmarshalWithKeyOrder([]byte(src))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}

	ctx := kongfig.WithRenderKeyOrderCtx(context.Background(), order)
	out, err := tomlparser.New(tomlparser.WithIndent("")).MarshalCtx(ctx, data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if string(out) != src {
		t.Errorf("round-trip changed the document:\n got:\n%s\nwant:\n%s", out, src)
	}
}

// The interface is what passthroughRenderer looks for, so a parser that gains
// or loses MarshalCtx should break the build here rather than silently stop
// seeing the render options.
var _ kongfig.CtxMarshaler = (*ctxMarshalParser)(nil)
