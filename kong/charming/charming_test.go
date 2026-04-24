package charming_test

import (
	"testing"

	"github.com/alecthomas/kong"
	charming "github.com/pmarschik/kongfig/kong/charming"
	"github.com/pmarschik/lipmark/theme"

	kongfig "github.com/pmarschik/kongfig"
)

func makeReg() *theme.Registry {
	reg := theme.NewWithOptions(theme.WithDefaults())
	reg.RegisterStruct("test", charming.LayerStyleDefs{
		Flags: theme.StyleDef{Foreground: "#9ece6a", Bold: true},
		Env:   theme.StyleDef{Foreground: "#7dcfff"},
	})
	return reg
}

func TestOptionsNonEmpty(t *testing.T) {
	kf := kongfig.New()
	reg := makeReg()
	opts := charming.Options(kf, reg, "test")
	if len(opts) == 0 {
		t.Fatal("Options returned empty slice")
	}
}

func TestOptionsIncludesResolver(t *testing.T) {
	kf := kongfig.New()
	reg := makeReg()
	opts := charming.Options(kf, reg, "test")

	// Verify options can be applied to a kong instance without error.
	type CLI struct {
		Host string `name:"host" default:"localhost" help:"Host."`
	}
	var cli CLI
	opts = append(opts, kong.Name("test"))
	_, err := kong.New(&cli, opts...)
	if err != nil {
		t.Fatalf("kong.New with Options() failed: %v", err)
	}
}

func TestStylerDefs(_ *testing.T) {
	reg := makeReg()
	// LayerStyleDefs and ConfigStyleDefs should register without panic.
	reg.RegisterStruct("test", charming.ConfigStyleDefs{
		Derived: theme.StyleDef{Foreground: "#e0af68"},
	})
}

func TestStylerFunc(t *testing.T) {
	reg := makeReg()
	s := charming.Styler(reg, "test")
	if s == nil {
		t.Fatal("Styler returned nil")
	}
	// Check it implements kongfig.Styler.
	var _ kongfig.Styler = s
}
