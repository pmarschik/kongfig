package kongfig_test

import (
	"context"
	"io"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

func TestNewFor_InjectsOmitEmptyPaths(t *testing.T) {
	type appConfig struct {
		Labels map[string]string `yaml:"labels,omitempty"  kongfig:"labels"`
		Name   string            `kongfig:"name"`
		Tags   []string          `kongfig:"tags,omitempty"`
	}

	kf := kongfig.NewFor[appConfig]()
	capture := &ctxCapture{}
	if err := kf.RenderWith(context.Background(), io.Discard, capture); err != nil {
		t.Fatal("render:", err)
	}

	got := kongfig.OmitEmptyKey.GetAll(capture.ctx)
	for _, want := range []string{"tags", "labels"} {
		if !got[want] {
			t.Errorf("omitempty paths = %v, want %q", got, want)
		}
	}
	if got["name"] {
		t.Errorf("untagged field published: %v", got)
	}
}
