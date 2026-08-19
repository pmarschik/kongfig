package kongfig_test

import (
	"context"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// plainData is ProviderData without position support.
type plainData struct{}

func (plainData) RenderAnnotation(context.Context, kongfig.Styler, string) string { return "" }

// positionedData is ProviderData that reports positions for known paths.
type positionedData struct {
	plainData
	pos map[string]kongfig.SourcePosition
}

func (d positionedData) PositionOf(path string) *kongfig.SourcePosition {
	p, ok := d.pos[path]
	if !ok {
		return nil
	}
	return &p
}

func TestLayerMeta_PositionOf_DelegatesToProviderData(t *testing.T) {
	meta := kongfig.LayerMeta{Data: positionedData{pos: map[string]kongfig.SourcePosition{
		"db.host": {File: "/etc/app/config.yaml", Line: 42, Col: 8},
	}}}

	got := meta.PositionOf("db.host")

	if got == nil {
		t.Fatal("PositionOf(db.host) = nil, want a position")
	}
	if got.File != "/etc/app/config.yaml" || got.Line != 42 || got.Col != 8 {
		t.Errorf("PositionOf(db.host) = %+v, want /etc/app/config.yaml:42:8", *got)
	}
}

func TestLayerMeta_PositionOf_UnknownPathIsNil(t *testing.T) {
	meta := kongfig.LayerMeta{Data: positionedData{pos: map[string]kongfig.SourcePosition{}}}

	if got := meta.PositionOf("db.host"); got != nil {
		t.Errorf("PositionOf(db.host) = %+v, want nil for an unrecorded path", *got)
	}
}

func TestLayerMeta_PositionOf_UnsupportedProviderDataIsNil(t *testing.T) {
	// Env and defaults layers have no document to point into.
	for name, meta := range map[string]kongfig.LayerMeta{
		"no data":              {},
		"data without support": {Data: plainData{}},
	} {
		if got := meta.PositionOf("db.host"); got != nil {
			t.Errorf("%s: PositionOf(db.host) = %+v, want nil", name, *got)
		}
	}
}

func TestSourcePosition_String(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
		pos  kongfig.SourcePosition
	}{
		{name: "file line col", pos: kongfig.SourcePosition{File: "config.yaml", Line: 42, Col: 8}, want: "config.yaml:42:8"},
		{name: "no column", pos: kongfig.SourcePosition{File: "config.yaml", Line: 42}, want: "config.yaml:42"},
		{name: "no line", pos: kongfig.SourcePosition{File: "config.yaml"}, want: "config.yaml"},
		{name: "empty", pos: kongfig.SourcePosition{}, want: ""},
	} {
		if got := tc.pos.String(); got != tc.want {
			t.Errorf("%s: String() = %q, want %q", tc.name, got, tc.want)
		}
	}
}
