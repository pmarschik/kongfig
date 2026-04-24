// Package main: feature demo — charming styled output with custom per-layer colors.
// Shows how to register a theme with LayerStyleDefs to color each source layer.
//
// Run:
//
//	go run ./example/features/charming
//	go run ./example/features/charming --plain   # disable colors
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
	kongcharming "github.com/pmarschik/kongfig/kong/charming"
	kongshow "github.com/pmarschik/kongfig/kong/show"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/style/plain"
	"github.com/pmarschik/lipmark/theme"
)

type AppConfig struct {
	Host  string `kongfig:"host"`
	Port  int    `kongfig:"port"`
	Debug bool   `kongfig:"debug"`
}

type CLI struct {
	Host  string               `name:"host"  default:"localhost" help:"Server hostname."`
	Show  kongshow.SimpleFlags `config:"-"   embed:""`
	Port  int                  `name:"port"  default:"8080"      help:"Listen port."`
	Debug bool                 `name:"debug" negatable:""        help:"Enable debug."`
	Plain bool                 `name:"plain" config:"-"          negatable:""            help:"Plain output (no colors)."`
}

var defaults = AppConfig{Host: "localhost", Port: 8080}

func main() {
	ctx := context.Background()
	kf := kongfig.New()
	kf.MustLoad(ctx, structsprovider.Defaults(defaults))

	// --- Feature: build a theme registry with custom per-layer colors ---
	reg := theme.NewWithOptions(theme.WithDefaults())
	reg.RegisterStruct("auto", kongcharming.LayerStyleDefs{
		Flags:    theme.StyleDef{Foreground: "#9ece6a", Bold: true}, // green
		Env:      theme.StyleDef{Foreground: "#7dcfff"},             // blue
		File:     theme.StyleDef{Foreground: "#bb9af7"},             // purple
		Defaults: theme.StyleDef{Foreground: "#565f89"},             // muted
	})
	// ---

	var cli CLI
	opts := []kong.Option{
		kong.Name("charming"),
		kong.Description("kongfig feature demo: charming styled output."),
		kong.UsageOnError(),
	}
	// --- Feature: kongcharming.Options wires the resolver + charming help formatter ---
	opts = append(opts, kongcharming.Options(kf, reg, "auto")...)
	// ---

	k, err := kong.New(&cli, opts...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := k.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var styler kongfig.Styler
	if cli.Plain {
		styler = plain.New()
	} else {
		// --- Feature: create styler from the same registry for consistent colors ---
		styler = kongcharming.Styler(reg, "auto")
		// ---
	}
	if err := cli.Show.Render(ctx, os.Stdout, kf, styler); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
