// Package main: feature demo — kong resolver + provider integration.
// Shows the full kong integration pattern: resolver wired into kong.New,
// env/args providers loaded after parse, and render options with flag/env names.
//
// Run:
//
//	go run ./example/features/kong
//	go run ./example/features/kong --host=prod.example.com --port=443
//	KONG_HOST=env.example.com go run ./example/features/kong
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
	"github.com/pmarschik/kongfig/style/plain"
)

type AppConfig struct {
	Host  string `kongfig:"host"`
	Port  int    `kongfig:"port"`
	Debug bool   `kongfig:"debug"`
}

// CLI mirrors AppConfig with kong flag tags.
// env:"" on CLI fields feeds kongprovider.Env(k) — kong reads the actual env values.
type CLI struct {
	Host  string               `name:"host"  env:"KONG_HOST"  default:"localhost" help:"Server hostname."`
	Show  kongshow.SimpleFlags `config:"-"   embed:""`
	Port  int                  `name:"port"  env:"KONG_PORT"  default:"8080"      help:"Listen port."`
	Debug bool                 `name:"debug" env:"KONG_DEBUG" negatable:""        help:"Enable debug."`
}

var defaults = AppConfig{Host: "localhost", Port: 8080}

var bgCtx = context.Background()

// setupKongfig loads defaults and env vars before kong parses flags.
// The env.tag load lets the kongfig resolver surface env-sourced defaults in kong's --help.
func setupKongfig() *kongfig.Kongfig {
	kf := kongfig.New()
	kf.MustLoad(bgCtx, structsprovider.Defaults(defaults))

	// --- Feature step 1: TagEnv pre-loads env vars before kong parses flags ---
	// This lets env vars show in the resolver so kong default-fills from them.
	kf.MustLoad(bgCtx, structsprovider.TagEnv[CLI]())
	// ---

	return kf
}

func main() {
	kf := setupKongfig()

	var cli CLI
	k, err := kong.New(&cli,
		kong.Name("kong"),
		kong.Description("kongfig feature demo: kong integration."),
		kong.UsageOnError(),
		// --- Feature step 2: wire kongfig as the resolver for kong flags ---
		// kong reads flag defaults from kongfig's merged map before user input.
		kong.Resolvers(kongresolver.New(kf)),
		// ---
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

	// --- Feature step 3: load actual env var values from kong's parsed state ---
	// env.kong intentionally overwrites env.tag (pre-loaded for resolver); silence the expected collision.
	kf.MustLoad(bgCtx, kongprovider.Env(k), kongfig.WithSilenceCollisions())
	// --- Feature step 4: load CLI flag values from kong's parse context ---
	kf.MustLoad(bgCtx, kongprovider.Args(ctx))
	// ---

	cfg, err := kongfig.Get[AppConfig](kf)
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}
	_ = cfg

	// Feature step 5: env var names and flag names auto-registered via ProviderFieldNamesSupport.
	if err := cli.Show.Render(bgCtx, os.Stdout, kf, plain.New()); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
