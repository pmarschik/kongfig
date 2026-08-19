package toml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

// orderingFixture mirrors the shape that exposed the bug downstream: a table
// that owns both a child table (which gets a [section] header) and an array of
// tables short enough to be written as a key/value line.
func orderingFixture() kongfig.ConfigData {
	return kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{
				"path":    "/dev",
				"buckets": kongfig.ConfigData{"work": kongfig.ConfigData{"path": "/w"}},
				"aux":     []any{kongfig.ConfigData{"path": "/aux"}},
			},
		},
	}
}

// developerTable digs out roots.developer, failing the test with the emitted
// document when the shape is not what the round trip should have preserved.
func developerTable(t *testing.T, got kongfig.ConfigData, doc []byte) kongfig.ConfigData {
	t.Helper()

	roots, ok := got["roots"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("roots = %T, want ConfigData:\n%s", got["roots"], doc)
	}
	dev, ok := roots["developer"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("roots.developer = %T, want ConfigData:\n%s", roots["developer"], doc)
	}
	return dev
}

func TestMarshal_ArrayOfTablesStaysWithItsParent(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("roots.*.buckets.*"))

	b, err := p.Marshal(orderingFixture())
	if err != nil {
		t.Fatal("marshal:", err)
	}
	got, err := p.Unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal %q: %v", b, err)
	}

	dev := developerTable(t, got, b)
	if _, found := dev["aux"]; !found {
		t.Errorf("roots.developer.aux lost in round trip:\n%s", b)
	}
	buckets, ok := dev["buckets"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("roots.developer.buckets = %T, want ConfigData:\n%s", dev["buckets"], b)
	}
	if _, stolen := buckets["aux"]; stolen {
		t.Errorf("aux re-parsed as a member of roots.developer.buckets:\n%s", b)
	}
}

// The same ordering rule applies with no inline marks at all: buckets then gets
// its own [section] header, and aux must still precede it.
func TestMarshal_ArrayOfTablesPrecedesChildSections(t *testing.T) {
	b, err := tomlparser.Default.Marshal(orderingFixture())
	if err != nil {
		t.Fatal("marshal:", err)
	}
	got, err := tomlparser.Default.Unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal %q: %v", b, err)
	}

	dev := developerTable(t, got, b)
	if _, found := dev["aux"]; !found {
		t.Errorf("roots.developer.aux lost in round trip:\n%s", b)
	}
}

func TestRender_ArrayOfTablesStaysWithItsParent(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("roots.*.buckets.*"))

	var buf bytes.Buffer
	ctx := kongfig.RenderNoCommentsKey.WithCtx(context.Background(), true)
	if err := p.Bind(plainStyler{}).Render(ctx, &buf, orderingFixture()); err != nil {
		t.Fatal("render:", err)
	}

	got, err := p.Unmarshal(buf.Bytes())
	if err != nil {
		t.Fatalf("rendered output does not parse as TOML: %v\n%s", err, buf.String())
	}
	dev := developerTable(t, got, buf.Bytes())
	if _, found := dev["aux"]; !found {
		t.Errorf("roots.developer.aux escaped its parent table:\n%s", buf.String())
	}
}

// A redacted array of tables must never expand into [[sections]] carrying the
// real values, however long it is.
func TestRender_RedactedTableArrayNeverExpands(t *testing.T) {
	data := kongfig.ConfigData{
		"credentials": kongfig.RenderedValue{
			Redacted:        true,
			RedactedDisplay: "***",
			// Nested tables inside the elements force the block form.
			Value: []any{kongfig.ConfigData{
				"user": "admin",
				"tls":  kongfig.ConfigData{"key": "s3cret"},
			}},
		},
	}

	var buf bytes.Buffer
	if err := tomlparser.Default.Bind(plainStyler{}).Render(context.Background(), &buf, data); err != nil {
		t.Fatal("render:", err)
	}

	out := buf.String()
	for _, leaked := range []string{"admin", "s3cret"} {
		if strings.Contains(out, leaked) {
			t.Errorf("redacted array leaked %q:\n%s", leaked, out)
		}
	}
	if !strings.Contains(out, "***") {
		t.Errorf("redacted placeholder missing:\n%s", out)
	}
}
