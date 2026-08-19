package kongfig_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

// loadTOML loads src as one layer, carrying the document order the parser
// reports, the way providers/file does.
func loadTOML(t *testing.T, kf *kongfig.Kongfig, name, src string) {
	t.Helper()
	data, order, err := tomlparser.Default.UnmarshalWithKeyOrder([]byte(src))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}
	if err := kf.LoadParsed(data, name, kongfig.WithLayerKeyOrder(order)); err != nil {
		t.Fatal("load:", err)
	}
}

// The order a single layer reported is the merged view's order too: a document
// says what its author wanted read first, and merging one layer cannot lose that.
func TestKeyOrder_SingleLayer_IsTheMergedOrder(t *testing.T) {
	kf := kongfig.New()
	loadTOML(t, kf, "file", "zebra = 1\napple = 2\n")

	got := kf.KeyOrder()[""]
	if strings.Join(got, ",") != "zebra,apple" {
		t.Errorf("merged root order = %v, want [zebra apple]", got)
	}
}

// The first layer to mention a key fixes where it reads; a later layer that
// re-states it must not move it, or an override would reshuffle the document.
func TestKeyOrder_LaterLayerKeepsTheFirstPosition(t *testing.T) {
	kf := kongfig.New()
	loadTOML(t, kf, "base", "zebra = 1\napple = 2\n")
	loadTOML(t, kf, "override", "apple = 3\nzebra = 4\n")

	got := kf.KeyOrder()[""]
	if strings.Join(got, ",") != "zebra,apple" {
		t.Errorf("merged root order = %v, want the base layer's [zebra apple]", got)
	}
}

// A key only the override introduces has no earlier position to keep, so it
// reads after the ones that do.
func TestKeyOrder_NewKeysAppendInLayerOrder(t *testing.T) {
	kf := kongfig.New()
	loadTOML(t, kf, "base", "zebra = 1\napple = 2\n")
	loadTOML(t, kf, "override", "mango = 3\nkiwi = 4\n")

	got := kf.KeyOrder()[""]
	if strings.Join(got, ",") != "zebra,apple,mango,kiwi" {
		t.Errorf("merged root order = %v, want [zebra apple mango kiwi]", got)
	}
}

// Nested parents are merged on their own terms: two layers each describing part
// of a table contribute to that table's order, not to the root's.
func TestKeyOrder_NestedParentsMergeSeparately(t *testing.T) {
	kf := kongfig.New()
	loadTOML(t, kf, "base", "[server]\nport = 1\n")
	loadTOML(t, kf, "override", "[server]\nhost = \"h\"\n")

	got := kf.KeyOrder()["server"]
	if strings.Join(got, ",") != "port,host" {
		t.Errorf("merged server order = %v, want [port host]", got)
	}
}

// A layer with nothing to say about order contributes nothing, rather than
// dropping the order the layers around it reported.
func TestKeyOrder_OrderlessLayerIsIgnored(t *testing.T) {
	kf := kongfig.New()
	loadTOML(t, kf, "file", "zebra = 1\napple = 2\n")
	if err := kf.LoadParsed(kongfig.ConfigData{"mango": 3}, "env"); err != nil {
		t.Fatal("load:", err)
	}

	got := kf.KeyOrder()[""]
	if strings.Join(got, ",") != "zebra,apple" {
		t.Errorf("merged root order = %v, want the file's [zebra apple]", got)
	}
}

// No layer reported an order, so there is none to report — the renderers fall
// back to their own default rather than to an empty list.
func TestKeyOrder_NoLayerReportsOrder_IsNil(t *testing.T) {
	kf := kongfig.New()
	if err := kf.LoadParsed(kongfig.ConfigData{"b": 1, "a": 2}, "env"); err != nil {
		t.Fatal("load:", err)
	}

	if got := kf.KeyOrder(); got != nil {
		t.Errorf("KeyOrder() = %v, want nil", got)
	}
}

// --- the merged render ---

type orderedBucket struct {
	Path  string `kongfig:"path"`
	Color string `kongfig:"color"`
}

// The schema declares the opposite of what the fixture document says, so a test
// that expects document order cannot be satisfied by struct field order.
// Suppressing govet keeps its fieldalignment autofix from reordering the fields,
// which would make these tests assert their own fixture rather than the merge.
//
//nolint:govet // declaration order is the subject of these tests
type orderedServer struct {
	Port int    `kongfig:"port"`
	Host string `kongfig:"host"`
}

//nolint:govet // declaration order is the subject of these tests
type orderedConfig struct {
	Server  orderedServer            `kongfig:"server"`
	Buckets map[string]orderedBucket `kongfig:"buckets"`
}

const orderedSrc = "[buckets]\n[buckets.work]\npath = \"/w\"\ncolor = \"blue\"\n\n[server]\nhost = \"h\"\nport = 8080\n"

// The merged view reads in the order the document was written, down to the keys
// of a map the schema cannot order and the fields of the struct inside it.
func TestMergedRender_FollowsDocumentOrder(t *testing.T) {
	kf := kongfig.NewFor[orderedConfig]()
	loadTOML(t, kf, "file", orderedSrc)

	var buf bytes.Buffer
	err := kf.RenderWith(context.Background(), &buf,
		tomlparser.Default.Bind(mockStyler{}), kongfig.WithRenderNoComments())
	if err != nil {
		t.Fatal("render:", err)
	}
	out := buf.String()

	// buckets before server, path before color, host before port — all three the
	// opposite of what schema order or the alphabet would give.
	for _, pair := range [][2]string{
		{"[buckets", "[server]"},
		{"path", "color"},
		{"host", "port"},
	} {
		if i, j := strings.Index(out, pair[0]), strings.Index(out, pair[1]); i < 0 || j < 0 || i > j {
			t.Errorf("expected %q before %q (%d, %d):\n%s", pair[0], pair[1], i, j, out)
		}
	}
}

// A key the document never mentioned still has a place: struct field order puts
// it where the schema declared it, ahead of the alphabetical fallback.
func TestMergedRender_SchemaOrdersWhatTheDocumentOmits(t *testing.T) {
	kf := kongfig.NewFor[orderedConfig]()
	// The document orders the root and the bucket, but says nothing about server.
	loadTOML(t, kf, "file", "[buckets.work]\npath = \"/w\"\n\n[server]\n")
	if err := kf.LoadParsed(
		kongfig.ConfigData{"server": kongfig.ConfigData{"host": "h", "port": 8080}}, "env",
	); err != nil {
		t.Fatal("load:", err)
	}

	var buf bytes.Buffer
	err := kf.RenderWith(context.Background(), &buf,
		tomlparser.Default.Bind(mockStyler{}), kongfig.WithRenderNoComments())
	if err != nil {
		t.Fatal("render:", err)
	}
	out := buf.String()

	if i, j := strings.Index(out, "port"), strings.Index(out, "host"); i < 0 || j < 0 || i > j {
		t.Errorf("expected schema order port before host (%d, %d):\n%s", i, j, out)
	}
}

// The call site has the last word: a caller that passes an order gets it, not a
// merge of what the layers happened to report.
func TestMergedRender_ExplicitKeyOrderStillWins(t *testing.T) {
	kf := kongfig.NewFor[orderedConfig]()
	loadTOML(t, kf, "file", orderedSrc)

	var buf bytes.Buffer
	err := kf.RenderWith(context.Background(), &buf,
		tomlparser.Default.Bind(mockStyler{}),
		kongfig.WithRenderNoComments(),
		kongfig.WithRenderKeyOrder(map[string][]string{"server": {"port", "host"}}))
	if err != nil {
		t.Fatal("render:", err)
	}
	out := buf.String()

	if i, j := strings.Index(out, "port"), strings.Index(out, "host"); i < 0 || j < 0 || i > j {
		t.Errorf("expected the explicit order port before host (%d, %d):\n%s", i, j, out)
	}
}
