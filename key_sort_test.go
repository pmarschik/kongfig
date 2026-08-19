package kongfig_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

type sortedRule struct {
	Path     string `kongfig:"path"`
	Priority int    `kongfig:"priority"`
}

type sortedConfig struct {
	Rules map[string]sortedRule `kongfig:"rules,sortby=-priority"`
}

// rulesTOML writes the three rules in alphabetical order, so a document order
// that beat the sortby mark would be visible as the alphabet.
const rulesTOML = `[rules.alpha]
path = "/a"
priority = 1

[rules.beta]
path = "/b"
priority = 3

[rules.gamma]
path = "/g"
priority = 2
`

// renderTOML renders kf as TOML, which puts each map entry in its own table
// header, so the order of the entries is the order of those headers.
func renderTOML(t *testing.T, kf *kongfig.Kongfig, opts ...kongfig.RenderOption) string {
	t.Helper()
	var buf bytes.Buffer
	opts = append([]kongfig.RenderOption{kongfig.WithRenderNoComments()}, opts...)
	if err := kf.RenderWith(context.Background(), &buf,
		tomlparser.Default.Bind(mockStyler{}), opts...); err != nil {
		t.Fatal("render:", err)
	}
	return buf.String()
}

// assertOrder checks that each needle reads before the next one.
func assertOrder(t *testing.T, out string, needles ...string) {
	t.Helper()
	for i := 0; i+1 < len(needles); i++ {
		a, b := strings.Index(out, needles[i]), strings.Index(out, needles[i+1])
		if a < 0 || b < 0 || a > b {
			t.Errorf("expected %q before %q (%d, %d):\n%s", needles[i], needles[i+1], a, b, out)
		}
	}
}

// A sortby= tag is the answer to "these entries have a priority field, read them
// by it": the map's keys carry no order of their own, and neither the alphabet
// nor the order the file was written in says which rule matters most.
func TestKeySort_SortByTagOrdersMapEntries(t *testing.T) {
	kf := kongfig.NewFor[sortedConfig]()
	loadTOML(t, kf, "file", rulesTOML)

	out := renderTOML(t, kf)
	assertOrder(t, out, "[rules.beta]", "[rules.gamma]", "[rules.alpha]")
}

// The mark outranks the order the document reported: the author of the file
// wrote the entries somewhere, but the schema says what orders them.
func TestKeySort_SortByBeatsDocumentOrder(t *testing.T) {
	kf := kongfig.NewFor[sortedConfig]()
	loadTOML(t, kf, "file", rulesTOML)

	if order := kf.KeyOrder()["rules"]; strings.Join(order, ",") != "alpha,beta,gamma" {
		t.Fatalf("fixture no longer reports document order [alpha beta gamma], got %v", order)
	}
	out := renderTOML(t, kf)
	assertOrder(t, out, "[rules.beta]", "[rules.alpha]")
}

// The keys inside each entry are still the schema's business — sorting the
// entries says nothing about the fields of one.
func TestKeySort_SortByLeavesEntryFieldsAlone(t *testing.T) {
	kf := kongfig.NewFor[sortedConfig]()
	loadTOML(t, kf, "file", rulesTOML)

	out := renderTOML(t, kf)
	assertOrder(t, out, "[rules.beta]", "path", "priority")
}

// A comparator handles what a tag cannot state, and has the last word over one.
func TestKeySort_ComparatorOverridesTag(t *testing.T) {
	kf := kongfig.NewFor[sortedConfig]()
	loadTOML(t, kf, "file", rulesTOML)

	out := renderTOML(t, kf, kongfig.WithRenderKeySort(
		func(path string, keys []string, _ kongfig.ConfigData) []string {
			if path != "rules" {
				return keys
			}
			return []string{"gamma", "alpha", "beta"}
		}))
	assertOrder(t, out, "[rules.gamma]", "[rules.alpha]", "[rules.beta]")
}

// A comparator that has no opinion about a path returns the keys it was handed,
// which is the sortby order — returning them unchanged must not undo the tag.
func TestKeySort_PassthroughComparatorKeepsTagOrder(t *testing.T) {
	kf := kongfig.NewFor[sortedConfig]()
	loadTOML(t, kf, "file", rulesTOML)

	out := renderTOML(t, kf, kongfig.WithRenderKeySort(
		func(_ string, keys []string, _ kongfig.ConfigData) []string { return keys }))
	assertOrder(t, out, "[rules.beta]", "[rules.gamma]", "[rules.alpha]")
}

// The call site still has the last word over both: a caller that passes an order
// for a parent gets exactly that order.
func TestKeySort_ExplicitOrderBeatsTagAndComparator(t *testing.T) {
	kf := kongfig.NewFor[sortedConfig]()
	loadTOML(t, kf, "file", rulesTOML)

	out := renderTOML(t, kf,
		kongfig.WithRenderKeyOrder(map[string][]string{"rules": {"gamma", "beta", "alpha"}}),
		kongfig.WithRenderKeySort(func(_ string, _ []string, _ kongfig.ConfigData) []string {
			return []string{"alpha", "beta", "gamma"}
		}))
	assertOrder(t, out, "[rules.gamma]", "[rules.beta]", "[rules.alpha]")
}

// Writing a config file goes through the same rules, so a file kongfig writes
// reads the way the same data renders.
func TestKeySort_SortByAppliesWhenMarshaling(t *testing.T) {
	ctx := kongfig.WithRenderKeySortByCtx(context.Background(),
		map[string]string{"rules": "-priority"})
	data := kongfig.ConfigData{"rules": kongfig.ConfigData{
		"alpha": kongfig.ConfigData{"priority": 1},
		"beta":  kongfig.ConfigData{"priority": 3},
		"gamma": kongfig.ConfigData{"priority": 2},
	}}

	b, err := tomlparser.Default.MarshalCtx(ctx, data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	assertOrder(t, string(b), "[rules.beta]", "[rules.gamma]", "[rules.alpha]")
}
