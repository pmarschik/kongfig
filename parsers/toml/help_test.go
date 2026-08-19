package toml_test

import (
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	render "github.com/pmarschik/kongfig/render"
)

func TestRender_HelpTextForATable_SitsAboveItsHeader(t *testing.T) {
	// A help text keyed on a table describes the table. Left to the prefix match
	// it would surface above whichever key the table happens to render first,
	// where it reads as documentation of that key instead.
	data := kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{"path": "/dev"},
		},
	}
	ctx := kongfig.WithRenderHelpTextsCtx(context.Background(), map[string]string{
		"roots": "the directory trees yard manages",
	})

	out := renderPlain(ctx, t, tomlparser.New(), data)

	want := "# the directory trees yard manages\n[roots]\n"
	if !strings.Contains(out, want) {
		t.Errorf("help text is not above the header\nwant substring:\n%q\ngot:\n%s", want, out)
	}
	// Spent above the header, so the keys underneath stay unannotated.
	if strings.Count(out, "the directory trees") != 1 {
		t.Errorf("help text was emitted more than once:\n%s", out)
	}
}

func TestRender_HelpTextForANestedTable_IsNotStolenByItsParent(t *testing.T) {
	data := kongfig.ConfigData{
		"roots": kongfig.ConfigData{
			"developer": kongfig.ConfigData{
				"buckets": kongfig.ConfigData{"work": kongfig.ConfigData{"dirname": "."}},
			},
		},
	}
	ctx := kongfig.WithRenderHelpTextsCtx(context.Background(), map[string]string{
		"roots.developer.buckets": "where repos are routed",
	})

	out := renderPlain(ctx, t, tomlparser.New(), data)

	// The comment takes the indentation of the header it belongs to.
	want := "    # where repos are routed\n    [roots.developer.buckets]\n"
	if !strings.Contains(out, want) {
		t.Errorf("nested help text is misplaced\nwant substring:\n%q\ngot:\n%s", want, out)
	}
}

func TestRender_HelpTextForATableArrayBlock_IsEmittedOnceAboveTheFirst(t *testing.T) {
	// The list is the thing being described, so the text belongs above the first
	// [[block]] rather than above every one of them.
	data := kongfig.ConfigData{
		"rules": []any{
			kongfig.ConfigData{"match": "a", "org": "one"},
			kongfig.ConfigData{"match": "b", "org": "two"},
		},
	}
	ctx := kongfig.WithRenderHelpTextsCtx(context.Background(), map[string]string{
		"rules": "rewrites applied before routing",
	})

	// A width no entry fits in forces the [[rules]] shape.
	out := renderPlain(render.WithTTYSizeCtx(ctx, 10, 0), t, tomlparser.New(), data)

	if !strings.Contains(out, "# rewrites applied before routing\n[[rules]]\n") {
		t.Errorf("help text is not above the first block:\n%s", out)
	}
	if got := strings.Count(out, "# rewrites applied before routing"); got != 1 {
		t.Errorf("help text emitted %d times, want 1:\n%s", got, out)
	}
}
