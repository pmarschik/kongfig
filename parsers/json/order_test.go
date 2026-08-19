package json_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
)

// A document rewritten through MarshalCtx comes back as it went in: the order
// the parser reported is the order the encoder writes, so editing a config file
// does not alphabetize it.
func TestJSONMarshalCtx_RoundTripsDocumentOrder(t *testing.T) {
	const src = "{\n  \"zebra\": 1,\n  \"apple\": 2,\n  \"server\": {\n    \"port\": 8080,\n    \"host\": \"h\"\n  }\n}\n"

	data, order, err := jsonparser.Default.UnmarshalWithKeyOrder([]byte(src))
	if err != nil {
		t.Fatal("unmarshal:", err)
	}

	out, err := jsonparser.Default.MarshalCtx(
		kongfig.WithRenderKeyOrderCtx(context.Background(), order), data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if string(out) != src {
		t.Errorf("round-trip changed the document:\n got:\n%s\nwant:\n%s", out, src)
	}
}

// Compact output follows the order too, without picking up the whitespace of
// the indented form.
func TestJSONMarshalCtx_CompactFollowsOrder(t *testing.T) {
	data := kongfig.ConfigData{"zebra": 1, "apple": kongfig.ConfigData{"b": 2, "a": 3}}
	order := map[string][]string{"": {"zebra", "apple"}, "apple": {"b", "a"}}

	out, err := jsonparser.Compact.MarshalCtx(
		kongfig.WithRenderKeyOrderCtx(context.Background(), order), data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if got, want := string(out), "{\"zebra\":1,\"apple\":{\"b\":2,\"a\":3}}\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// An order that covers only part of the document still places what it names;
// the rest follows alphabetically rather than being dropped.
func TestJSONMarshalCtx_PartialOrderKeepsEveryKey(t *testing.T) {
	data := kongfig.ConfigData{"zebra": 1, "apple": 2, "mango": 3}

	out, err := jsonparser.Compact.MarshalCtx(
		kongfig.WithRenderKeyOrderCtx(context.Background(),
			map[string][]string{"": {"zebra"}}), data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if got, want := string(out), "{\"zebra\":1,\"apple\":2,\"mango\":3}\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// Without an order there is nothing to honor, so the ctx-aware path must be
// the plain one byte for byte — nested maps, slices, and escaping included.
func TestJSONMarshalCtx_WithoutOrderMatchesMarshal(t *testing.T) {
	data := kongfig.ConfigData{
		"zebra":  1,
		"apple":  "a <tag> & more",
		"nested": kongfig.ConfigData{"b": true, "a": []any{1, kongfig.ConfigData{"deep": "v"}}},
		"empty":  kongfig.ConfigData{},
	}

	for _, p := range []*jsonparser.Parser{jsonparser.Default, jsonparser.Compact} {
		plain, err := p.Marshal(data)
		if err != nil {
			t.Fatal("marshal:", err)
		}
		ctxOut, err := p.MarshalCtx(context.Background(), data)
		if err != nil {
			t.Fatal("marshal ctx:", err)
		}
		if !bytes.Equal(plain, ctxOut) {
			t.Errorf("MarshalCtx without an order diverged:\n got:\n%s\nwant:\n%s", ctxOut, plain)
		}
	}
}

// An ordered nested map, a slice of maps, and an empty map all have to survive
// the ordered walk: it replaces the encoder for containers, not just the root.
func TestJSONMarshalCtx_OrderedWalkHandlesEveryShape(t *testing.T) {
	data := kongfig.ConfigData{
		"list":  []any{kongfig.ConfigData{"b": 1, "a": 2}},
		"empty": kongfig.ConfigData{},
		"deep":  kongfig.ConfigData{"inner": kongfig.ConfigData{"z": 1}},
	}
	order := map[string][]string{"": {"list", "empty", "deep"}}

	out, err := jsonparser.Default.MarshalCtx(
		kongfig.WithRenderKeyOrderCtx(context.Background(), order), data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	const want = "{\n  \"list\": [\n    {\n      \"a\": 2,\n      \"b\": 1\n    }\n  ],\n" +
		"  \"empty\": {},\n  \"deep\": {\n    \"inner\": {\n      \"z\": 1\n    }\n  }\n}\n"
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

var _ kongfig.CtxMarshaler = jsonparser.Parser{}

// A sortby mark orders keys just as a document order does, so the ctx-aware path
// has to take the ordered walk for it too — the fast path asks whether anything
// in the context orders keys, not whether one particular option is set.
func TestJSONMarshalCtx_HonorsSortBy(t *testing.T) {
	ctx := kongfig.WithRenderKeySortByCtx(context.Background(),
		map[string]string{"rules": "-priority"})
	data := kongfig.ConfigData{"rules": kongfig.ConfigData{
		"alpha": kongfig.ConfigData{"priority": 1},
		"beta":  kongfig.ConfigData{"priority": 3},
		"gamma": kongfig.ConfigData{"priority": 2},
	}}

	out, err := jsonparser.Default.MarshalCtx(ctx, data)
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
