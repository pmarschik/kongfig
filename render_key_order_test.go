package kongfig_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/render"
	schema "github.com/pmarschik/kongfig/schema"
)

// --- schema.FieldOrderPaths ---

type orderTop struct {
	Zed   string `kongfig:"zed"`
	Alpha string `kongfig:"alpha"`
	Mid   string `kongfig:"mid"`
}

func TestFieldOrderPaths_RootOrder(t *testing.T) {
	got := schema.FieldOrderPaths[orderTop]()
	want := []string{"zed", "alpha", "mid"}
	if got[""] == nil {
		t.Fatal("expected root entry in FieldOrderPaths")
	}
	if strings.Join(got[""], ",") != strings.Join(want, ",") {
		t.Errorf("root order: got %v, want %v", got[""], want)
	}
}

type orderNested struct {
	Beta  string     `kongfig:"beta"`
	Inner orderInner `kongfig:"inner"`
	Alpha string     `kongfig:"alpha"`
}

type orderInner struct {
	Y string `kongfig:"y"`
	X string `kongfig:"x"`
}

func TestFieldOrderPaths_Nested(t *testing.T) {
	got := schema.FieldOrderPaths[orderNested]()

	wantRoot := []string{"beta", "inner", "alpha"}
	if strings.Join(got[""], ",") != strings.Join(wantRoot, ",") {
		t.Errorf("root order: got %v, want %v", got[""], wantRoot)
	}

	wantInner := []string{"y", "x"}
	if strings.Join(got["inner"], ",") != strings.Join(wantInner, ",") {
		t.Errorf("inner order: got %v, want %v", got["inner"], wantInner)
	}
}

type orderEmbedded struct {
	orderEmbedInner
	Z string `kongfig:"z"`
}

type orderEmbedInner struct {
	B string `kongfig:"b"`
	A string `kongfig:"a"`
}

func TestFieldOrderPaths_EmbeddedSquashed(t *testing.T) {
	got := schema.FieldOrderPaths[orderEmbedded]()
	// Embedded (anonymous) fields are inlined into parent.
	want := []string{"b", "a", "z"}
	if strings.Join(got[""], ",") != strings.Join(want, ",") {
		t.Errorf("embedded root order: got %v, want %v", got[""], want)
	}
}

// --- render.OrderedKeys ---

func TestOrderedKeys_NoOrder_AlphaFallback(t *testing.T) {
	ctx := context.Background()
	data := kongfig.ConfigData{"c": 1, "a": 2, "b": 3}
	got := render.OrderedKeys(ctx, "", data)
	want := []string{"a", "b", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("alpha fallback: got %v, want %v", got, want)
	}
}

func TestOrderedKeys_WithOrder(t *testing.T) {
	ro := ctxWithKeyOrder(map[string][]string{"": {"c", "a", "b"}})
	data := kongfig.ConfigData{"c": 1, "a": 2, "b": 3}
	got := render.OrderedKeys(ro, "", data)
	want := []string{"c", "a", "b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ordered: got %v, want %v", got, want)
	}
}

func TestOrderedKeys_ExtraKeysAppendedAlpha(t *testing.T) {
	ro := ctxWithKeyOrder(map[string][]string{"": {"c", "a"}})
	data := kongfig.ConfigData{"c": 1, "a": 2, "b": 3, "d": 4}
	got := render.OrderedKeys(ro, "", data)
	// c, a from order, then b and d alpha
	want := []string{"c", "a", "b", "d"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("extra keys: got %v, want %v", got, want)
	}
}

// ctxWithKeyOrder returns a context with RenderKeyOrderKey set to orders.
func ctxWithKeyOrder(orders map[string][]string) context.Context {
	return kongfig.RenderKeyOrderKey.WithCtx(context.Background(), orders)
}

// --- NewFor field order in YAML render ---

type renderOrderConfig struct {
	Zed   string `kongfig:"zed"`
	Alpha string `kongfig:"alpha"`
	Mid   string `kongfig:"mid"`
}

func TestNewFor_FieldOrderInYAMLRender(t *testing.T) {
	ctx := context.Background()
	kf := kongfig.NewFor[renderOrderConfig]()
	kf.MustLoad(ctx, structsprovider.Defaults(renderOrderConfig{
		Zed:   "z",
		Alpha: "a",
		Mid:   "m",
	}))

	var buf bytes.Buffer
	if err := kf.RenderWith(ctx, &buf, yamlparser.Default.Bind(mockStyler{})); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	posZed := strings.Index(out, "zed")
	posAlpha := strings.Index(out, "alpha")
	posMid := strings.Index(out, "mid")

	if posZed < 0 || posAlpha < 0 || posMid < 0 {
		t.Fatalf("expected zed, alpha, mid in output, got:\n%s", out)
	}
	// Struct order: zed < alpha < mid
	if posZed >= posAlpha || posAlpha >= posMid {
		t.Errorf("expected struct field order zed<alpha<mid, positions: zed=%d alpha=%d mid=%d\noutput:\n%s",
			posZed, posAlpha, posMid, out)
	}
}

// --- YAML parser UnmarshalWithKeyOrder ---

func TestYAML_UnmarshalWithKeyOrder(t *testing.T) {
	input := "c: 1\na: 2\nb: 3\n"
	_, order, err := yamlparser.Default.UnmarshalWithKeyOrder([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	got := order[""]
	want := []string{"c", "a", "b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("yaml key order: got %v, want %v", got, want)
	}
}

func TestYAML_UnmarshalWithKeyOrder_Nested(t *testing.T) {
	input := "z: 1\ninner:\n  y: 2\n  x: 3\na: 4\n"
	_, order, err := yamlparser.Default.UnmarshalWithKeyOrder([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := []string{"z", "inner", "a"}
	if strings.Join(order[""], ",") != strings.Join(wantRoot, ",") {
		t.Errorf("yaml root order: got %v, want %v", order[""], wantRoot)
	}
	wantInner := []string{"y", "x"}
	if strings.Join(order["inner"], ",") != strings.Join(wantInner, ",") {
		t.Errorf("yaml inner order: got %v, want %v", order["inner"], wantInner)
	}
}

// --- TOML parser UnmarshalWithKeyOrder ---

func TestTOML_UnmarshalWithKeyOrder(t *testing.T) {
	input := "c = 1\na = 2\nb = 3\n"
	_, order, err := tomlparser.Default.UnmarshalWithKeyOrder([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	got := order[""]
	want := []string{"c", "a", "b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("toml key order: got %v, want %v", got, want)
	}
}

func TestTOML_UnmarshalWithKeyOrder_Nested(t *testing.T) {
	input := "z = 1\na = 4\n[inner]\ny = 2\nx = 3\n"
	_, order, err := tomlparser.Default.UnmarshalWithKeyOrder([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := []string{"z", "a", "inner"}
	if strings.Join(order[""], ",") != strings.Join(wantRoot, ",") {
		t.Errorf("toml root order: got %v, want %v", order[""], wantRoot)
	}
	wantInner := []string{"y", "x"}
	if strings.Join(order["inner"], ",") != strings.Join(wantInner, ",") {
		t.Errorf("toml inner order: got %v, want %v", order["inner"], wantInner)
	}
}

// --- JSON parser UnmarshalWithKeyOrder ---

func TestJSON_UnmarshalWithKeyOrder(t *testing.T) {
	input := `{"c": 1, "a": 2, "b": 3}`
	_, order, err := jsonparser.Default.UnmarshalWithKeyOrder([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	got := order[""]
	want := []string{"c", "a", "b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("json key order: got %v, want %v", got, want)
	}
}

func TestJSON_UnmarshalWithKeyOrder_Nested(t *testing.T) {
	input := `{"z": 1, "inner": {"y": 2, "x": 3}, "a": 4}`
	_, order, err := jsonparser.Default.UnmarshalWithKeyOrder([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := []string{"z", "inner", "a"}
	if strings.Join(order[""], ",") != strings.Join(wantRoot, ",") {
		t.Errorf("json root order: got %v, want %v", order[""], wantRoot)
	}
	wantInner := []string{"y", "x"}
	if strings.Join(order["inner"], ",") != strings.Join(wantInner, ",") {
		t.Errorf("json inner order: got %v, want %v", order["inner"], wantInner)
	}
}

// --- WithRenderKeyOrder explicitly ---

func TestWithRenderKeyOrder_YAML(t *testing.T) {
	ctx := context.Background()
	kf := kongfig.New()
	data := kongfig.ConfigData{"c": "cv", "a": "av", "b": "bv"}
	if err := kf.LoadParsed(data, "test"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	order := map[string][]string{"": {"c", "a", "b"}}
	err := kf.RenderWith(ctx, &buf, yamlparser.Default.Bind(mockStyler{}),
		kongfig.WithRenderKeyOrder(order))
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	posC := strings.Index(out, "c:")
	posA := strings.Index(out, "a:")
	posB := strings.Index(out, "b:")

	if posC < 0 || posA < 0 || posB < 0 {
		t.Fatalf("missing keys in output:\n%s", out)
	}
	if posC >= posA || posA >= posB {
		t.Errorf("expected c<a<b order, positions: c=%d a=%d b=%d\noutput:\n%s",
			posC, posA, posB, out)
	}
}
