// Package main: feature demo — per-layer config breakdown and layer filtering.
// --layers shows each source (defaults, env, flags) independently.
// --defaults / --no-env / --no-flags control which layers appear.
//
// Run:
//
//	go run ./example/features/layers
//	go run ./example/features/layers --layers
//	go run ./example/features/layers --defaults --layers
//	go run ./example/features/layers --host=override --layers
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
	kongprovider "github.com/pmarschik/kongfig/kong/provider"
	kongresolver "github.com/pmarschik/kongfig/kong/resolver"
	kongshow "github.com/pmarschik/kongfig/kong/show"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	render "github.com/pmarschik/kongfig/render"
	"github.com/pmarschik/kongfig/style/plain"
)

type AppConfig struct {
	Host  string `kongfig:"host"`
	Port  int    `kongfig:"port"`
	Debug bool   `kongfig:"debug"`
}

type CLI struct {
	Host string `name:"host" env:"LAYERS_HOST" default:"localhost" help:"Server hostname."`

	// --- Feature: embed kongshow.Flags for --format, --layers, --redacted ---
	Show kongshow.Flags `config:"-" embed:""`
	// ---

	Port  int  `name:"port"  env:"LAYERS_PORT" default:"8080"       help:"Listen port."`
	Debug bool `name:"debug" negatable:""      help:"Enable debug."`

	// --- Feature: per-layer visibility flags ---
	ShowDefaults bool `name:"defaults" config:"-" default:"false" negatable:"" help:"Show defaults layer."`
	ShowEnv      bool `name:"env"      config:"-" default:"true"  negatable:"" help:"Show env layer."`
	ShowFlags    bool `name:"flags"    config:"-" default:"true"  negatable:"" help:"Show flags layer."`
	// ---
}

var defaults = AppConfig{Host: "localhost", Port: 8080}

func main() {
	bgCtx := context.Background()
	kf := kongfig.New()
	kf.MustLoad(bgCtx, structsprovider.Defaults(defaults))
	kf.MustLoad(bgCtx, structsprovider.TagEnv[CLI]())

	var cli CLI
	k, err := kong.New(&cli,
		kong.Name("layers"),
		kong.Description("kongfig feature demo: per-layer breakdown."),
		kong.UsageOnError(),
		kongshow.FlagsVars(kongshow.WithLoaderFormats([]string{"yaml"})),
		kong.Resolvers(kongresolver.New(kf)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, err := k.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	kf.MustLoad(bgCtx, kongprovider.Env(k), kongfig.WithSilenceCollisions())
	kf.MustLoad(bgCtx, kongprovider.Args(ctx))

	// --layers is handled inside cli.Show.Render via the embedded LayersFlag.
	if err := cli.Show.Render(bgCtx, os.Stdout, kf, plain.New(),
		// --- Feature: BuildFilterSource converts show-bools to a "no-X" filter slice ---
		kongfig.WithRenderFilterSource(render.BuildFilterSource(map[string]bool{
			"defaults": cli.ShowDefaults,
			"env":      cli.ShowEnv,
			"flags":    cli.ShowFlags,
		})),
		// ---
	); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
