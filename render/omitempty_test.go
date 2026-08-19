package render_test

import (
	"context"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/render"
)

func omitCtx(t *testing.T, paths ...string) context.Context {
	t.Helper()

	marks := make(map[string]bool, len(paths))
	for _, p := range paths {
		marks[p] = true
	}
	return kongfig.OmitEmptyKey.WithCtx(context.Background(), marks)
}

// A marked path is dropped only when it actually holds nothing; the zero values
// of unmarked paths still belong in the output.
func TestOmitEmpty_OnlyMarkedEmptyValues(t *testing.T) {
	ctx := omitCtx(t, "a", "b")

	cases := []struct {
		v    any
		path string
		want bool
	}{
		{path: "a", v: "", want: true},
		{path: "a", v: 0, want: true},
		{path: "a", v: false, want: true},
		{path: "a", v: nil, want: true},
		{path: "a", v: []any{}, want: true},
		{path: "a", v: []string{}, want: true},
		{path: "a", v: kongfig.ConfigData{}, want: true},
		{path: "a", v: kongfig.RenderedValue{Value: ""}, want: true},
		{path: "a", v: "x", want: false},
		{path: "a", v: 1, want: false},
		{path: "a", v: true, want: false},
		{path: "a", v: []any{1}, want: false},
		{path: "c", v: "", want: false},
	}
	for _, c := range cases {
		if got := render.OmitEmpty(ctx, c.path, c.v); got != c.want {
			t.Errorf("OmitEmpty(%q, %#v) = %v, want %v", c.path, c.v, got, c.want)
		}
	}
}

// A redacted placeholder stands in for a value rather than being one, so it is
// never dropped as empty — the reader would otherwise lose the fact that the
// key is set.
func TestOmitEmpty_KeepsRedactedPlaceholder(t *testing.T) {
	ctx := omitCtx(t, "token")
	rv := kongfig.RenderedValue{Redacted: true, RedactedDisplay: "***"}

	if render.OmitEmpty(ctx, "token", rv) {
		t.Error("redacted placeholder dropped as empty")
	}
}

// Marks collected from map value types carry a "*" segment, which matches one
// segment of a concrete path.
func TestOmitEmpty_MatchesWildcardSegment(t *testing.T) {
	ctx := omitCtx(t, "profiles.*.push")

	if !render.OmitEmpty(ctx, "profiles.work.push", "") {
		t.Error("wildcard mark did not match a concrete path")
	}
	if render.OmitEmpty(ctx, "profiles.work.deep.push", "") {
		t.Error("wildcard segment matched more than one segment")
	}
}

// Without marks the helper costs a lookup and answers no.
func TestOmitEmpty_NoMarks(t *testing.T) {
	if render.OmitEmpty(context.Background(), "a", "") {
		t.Error("value dropped without any mark")
	}
}
