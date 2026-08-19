package kongfig_test

import (
	"context"
	"io"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// helpConfig carries help= annotations on its kongfig tags.
type helpConfig struct {
	Host string `kongfig:"host,help='hostname to listen on'"`
	Port int    `kongfig:"port"`
}

// renderHelpTexts renders kf with a capturing renderer and returns the help
// texts the renderer saw in its context.
func renderHelpTexts(t *testing.T, kf *kongfig.Kongfig, opts ...kongfig.RenderOption) map[string]string {
	t.Helper()
	capture := &ctxCapture{}
	if err := kf.RenderWith(context.Background(), io.Discard, capture, opts...); err != nil {
		t.Fatal("render:", err)
	}
	texts, _ := kongfig.RenderHelpTextsKey.Read(capture.ctx)
	return texts
}

func TestNewFor_InjectsHelpTexts(t *testing.T) {
	got := renderHelpTexts(t, kongfig.NewFor[helpConfig]())

	if got["host"] != "hostname to listen on" {
		t.Errorf("help texts = %v, want host -> %q", got, "hostname to listen on")
	}
}

func TestNewFor_HelpTexts_CallOptionWins(t *testing.T) {
	kf := kongfig.NewFor[helpConfig]()

	got := renderHelpTexts(t, kf, kongfig.WithRenderHelpTexts(map[string]string{"port": "TCP port"}))

	if _, ok := got["host"]; ok {
		t.Errorf("call-time help texts should replace the derived set, got %v", got)
	}
	if got["port"] != "TCP port" {
		t.Errorf("help texts = %v, want port -> %q", got, "TCP port")
	}
}

func TestNewFor_HelpTexts_SeenSetIsFreshPerRender(t *testing.T) {
	kf := kongfig.NewFor[helpConfig]()

	capture := &ctxCapture{}
	if err := kf.RenderWith(context.Background(), io.Discard, capture); err != nil {
		t.Fatal("first render:", err)
	}
	seen, ok := kongfig.RenderHelpTextsSeenKey.Read(capture.ctx)
	if !ok || seen == nil {
		t.Fatal("expected a seen-set alongside the derived help texts")
	}
	(*seen)["host"] = true

	if err := kf.RenderWith(context.Background(), io.Discard, capture); err != nil {
		t.Fatal("second render:", err)
	}
	next, _ := kongfig.RenderHelpTextsSeenKey.Read(capture.ctx)
	if next == nil || len(*next) != 0 {
		t.Errorf("second render reused the first render's seen-set: %v", next)
	}
}

func TestWithHelpTexts_AppliedAsRenderDefault(t *testing.T) {
	kf := kongfig.New(kongfig.WithHelpTexts(map[string]string{"host": "explicit"}))

	got := renderHelpTexts(t, kf)

	if got["host"] != "explicit" {
		t.Errorf("help texts = %v, want host -> %q", got, "explicit")
	}
}
