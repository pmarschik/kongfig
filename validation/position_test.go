package validation_test

import (
	"context"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/validation"
)

// positionedData is ProviderData that knows where each path lives.
type positionedData struct {
	pos map[string]kongfig.SourcePosition
}

func (positionedData) RenderAnnotation(context.Context, kongfig.Styler, string) string { return "" }

func (d positionedData) PositionOf(path string) *kongfig.SourcePosition {
	p, ok := d.pos[path]
	if !ok {
		return nil
	}
	return &p
}

// envData is ProviderData for a source with no document to point into.
type envData struct{}

func (envData) RenderAnnotation(context.Context, kongfig.Styler, string) string { return "" }

func positionedSource() *kongfig.SourceMeta {
	return &kongfig.SourceMeta{Layer: kongfig.LayerMeta{Data: positionedData{
		pos: map[string]kongfig.SourcePosition{
			"db.host": {File: "/etc/app/config.yaml", Line: 42, Col: 8},
		},
	}}}
}

func TestPathSource_Position(t *testing.T) {
	ps := validation.PathSource{Path: "db.host", Source: positionedSource()}

	got := ps.Position()
	if got == nil {
		t.Fatal("Position() = nil, want a position")
	}
	if want := "/etc/app/config.yaml:42:8"; got.String() != want {
		t.Errorf("Position() = %q, want %q", got.String(), want)
	}
}

func TestPathSource_Position_UnavailableIsNil(t *testing.T) {
	for name, ps := range map[string]validation.PathSource{
		"no provenance":  {Path: "db.host"},
		"env layer":      {Path: "db.host", Source: &kongfig.SourceMeta{Layer: kongfig.LayerMeta{Data: envData{}}}},
		"unlisted path":  {Path: "db.port", Source: positionedSource()},
		"empty pathspec": {Source: positionedSource()},
	} {
		if got := ps.Position(); got != nil {
			t.Errorf("%s: Position() = %+v, want nil", name, *got)
		}
	}
}
