package kongfig_test

import (
	"context"
	"io"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// ctxCapture is a Renderer that records the render context it was handed.
type ctxCapture struct{ ctx context.Context }

func (c *ctxCapture) Render(ctx context.Context, _ io.Writer, _ kongfig.ConfigData) error {
	c.ctx = ctx
	return nil
}

func TestNewFor_InjectsInlineTablePaths(t *testing.T) {
	type bucket struct {
		Path string `kongfig:"path"`
	}
	type appConfig struct {
		Buckets map[string]bucket `kongfig:"buckets,inline=2"`
	}

	kf := kongfig.NewFor[appConfig]()
	capture := &ctxCapture{}
	if err := kf.RenderWith(context.Background(), io.Discard, capture); err != nil {
		t.Fatal("render:", err)
	}

	got := kongfig.InlineTablesKey.GetAll(capture.ctx)
	if n, ok := got["buckets.*"]; !ok || n != 2 {
		t.Errorf("inline paths = %v, want buckets.* -> 2", got)
	}
}
