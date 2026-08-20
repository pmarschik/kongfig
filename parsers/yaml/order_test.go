package yaml_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
)

// A document rewritten through MarshalCtx comes back as it went in: the order
// the parser reported is the order the encoder writes, so editing a config file
// does not alphabetize it.
func TestYAMLMarshalCtx_RoundTripsDocumentOrder(t *testing.T) {
	const src = "zebra: 1\napple: 2\nserver:\n  port: 8080\n  host: h\n"

	data, order, err := yamlparser.Default.UnmarshalWithKeyOrder([]byte(src))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}

	out, err := yamlparser.Default.MarshalCtx(
		kongfig.WithRenderKeyOrderCtx(context.Background(), order), data,
	)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if string(out) != src {
		t.Errorf("round-trip changed the document:\n got:\n%s\nwant:\n%s", out, src)
	}
}

// An order that covers only part of the document still places what it names;
// the rest keeps the encoder's own ordering rather than being dropped.
func TestYAMLMarshalCtx_PartialOrderKeepsEveryKey(t *testing.T) {
	data := kongfig.ConfigData{"zebra": 1, "apple": 2, "mango": 3}

	out, err := yamlparser.Default.MarshalCtx(
		kongfig.WithRenderKeyOrderCtx(context.Background(),
			map[string][]string{"": {"zebra"}}), data,
	)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if got, want := string(out), "zebra: 1\napple: 2\nmango: 3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// Without an order there is nothing to honor, so the ctx-aware path must be
// the plain one byte for byte — every existing caller renders the same output.
func TestYAMLMarshalCtx_WithoutOrderMatchesMarshal(t *testing.T) {
	data := kongfig.ConfigData{
		"zebra":  1,
		"apple":  "two words",
		"nested": kongfig.ConfigData{"b": true, "a": []any{1, 2}},
		"a10":    1,
		"a9":     2,
	}

	plain, err := yamlparser.Default.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	ctxOut, err := yamlparser.Default.MarshalCtx(context.Background(), data)
	if err != nil {
		t.Fatal("marshal ctx:", err)
	}
	if !bytes.Equal(plain, ctxOut) {
		t.Errorf("MarshalCtx without an order diverged:\n got:\n%s\nwant:\n%s", ctxOut, plain)
	}
}

var _ kongfig.CtxMarshaler = yamlparser.Parser{}

// A sortby mark orders keys just as a document order does, so the ctx-aware path
// has to take the ordered walk for it too — the fast path asks whether anything
// in the context orders keys, not whether one particular option is set.
func TestYAMLMarshalCtx_HonorsSortBy(t *testing.T) {
	ctx := kongfig.WithRenderKeySortByCtx(context.Background(),
		map[string]string{"rules": "-priority"})
	data := kongfig.ConfigData{"rules": kongfig.ConfigData{
		"alpha": kongfig.ConfigData{"priority": 1},
		"beta":  kongfig.ConfigData{"priority": 3},
		"gamma": kongfig.ConfigData{"priority": 2},
	}}

	out, err := yamlparser.Default.MarshalCtx(ctx, data)
	if err != nil {
		t.Fatal("marshal ctx:", err)
	}
	if got := entryOrder(string(out), "alpha", "beta", "gamma"); got != "beta,gamma,alpha" {
		t.Errorf("entry order = %s, want beta,gamma,alpha:\n%s", got, out)
	}
}

// entryOrder returns the names in the order they appear in out, so a test can
// state the order it expects without indexing by hand.
func entryOrder(out string, names ...string) string {
	found := make([]string, 0, len(names))
	sorted := slices.SortedFunc(slices.Values(names), func(a, b string) int {
		return strings.Index(out, a) - strings.Index(out, b)
	})
	for _, n := range sorted {
		if strings.Contains(out, n) {
			found = append(found, n)
		}
	}
	return strings.Join(found, ",")
}
