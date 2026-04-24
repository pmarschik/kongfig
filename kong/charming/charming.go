// Package charming wires kong-charming's styled help renderer and kongfig's
// charming Styler into a kong application through a single Options() call.
package charming

import (
	"github.com/alecthomas/kong"
	kongcharming "github.com/pmarschik/kong-charming"
	kongfig "github.com/pmarschik/kongfig"
	kongresolver "github.com/pmarschik/kongfig/kong/resolver"
	charmingstyler "github.com/pmarschik/kongfig/style/charming"
	"github.com/pmarschik/lipmark/theme"
)

// ConfigStyleDefs holds style definitions for config value rendering.
// Register with [theme.Registry.RegisterStruct] to customize:
//
//	reg.RegisterStruct("auto", charming.ConfigStyleDefs{
//	    Derived: theme.StyleDef{Foreground: "#e0af68"},
//	})
type ConfigStyleDefs = charmingstyler.ConfigStyleDefs

// LayerStyleDefs holds per-layer annotation color definitions.
// Register with [theme.Registry.RegisterStruct] to customize per-source colors:
//
//	reg.RegisterStruct("auto", charming.LayerStyleDefs{
//	    Flags:    theme.StyleDef{Foreground: "#9ece6a", Bold: true},
//	    Env:      theme.StyleDef{Foreground: "#7dcfff"},
//	    File:     theme.StyleDef{Foreground: "#bb9af7"},
//	    Defaults: theme.StyleDef{Foreground: "#565f89"},
//	})
type LayerStyleDefs = charmingstyler.LayerStyleDefs

// Options returns a slice of kong.Option that configures:
//   - kong-charming's styled help renderer (using reg + themeName)
//   - a kongfig resolver seeded from k so flag defaults come from config layers
//
// Usage:
//
//	opts = append(opts, charming.Options(kf, reg, "auto")...)
func Options(kf *kongfig.Kongfig, reg *theme.Registry, themeName string) []kong.Option {
	t := reg.Resolve(themeName)
	styler := charmingstyler.New(t)
	_ = styler // available for future config display option

	return []kong.Option{
		kongcharming.KongOptions(kongcharming.WithHelpTheme(t)),
		kong.Resolvers(kongresolver.New(kf)),
	}
}

// Styler returns a kongfig.Styler backed by the resolved theme set.
// Useful when you need a Styler for rendering outside of kong parsing.
func Styler(reg *theme.Registry, themeName string) *charmingstyler.Styler {
	return charmingstyler.New(reg.Resolve(themeName))
}
