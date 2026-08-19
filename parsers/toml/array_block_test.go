package toml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	"github.com/pmarschik/kongfig/render"
)

// An array broken one element per line keeps its provenance beside the opening
// bracket: the comment belongs to the key, and a line of its own above the key
// reads as a comment on the block that follows.
func TestRender_ArrayBlock_AnnotationSitsAfterTheOpeningBracket(t *testing.T) {
	data := kongfig.ConfigData{
		"archive": sourced([]any{"/srv/one", "/srv/two", "/srv/three"}, "file"),
	}

	var buf bytes.Buffer
	ctx := render.WithTTYSizeCtx(context.Background(), 30, 0)
	if err := tomlparser.Default.Bind(plainStyler{}).Render(ctx, &buf, data); err != nil {
		t.Fatal("render:", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if !strings.HasPrefix(lines[0], "archive = [") {
		t.Fatalf("array did not go one element per line:\n%s", buf.String())
	}
	if !strings.Contains(lines[0], "# file") {
		t.Errorf("annotation is not beside the bracket:\n%s", buf.String())
	}
}
