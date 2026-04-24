// Package main: common kongfig setup — charming output, XDG discovery, env, file, layers, redacted.
// Uses default charming theme colors; no custom LayerStyleDefs.
// This is the pattern to copy for a typical production app.
//
// Run:
//
//	go run ./example/common
//	go run ./example/common --config=example/common/config.yaml
//	go run ./example/common --config=example/common/config.toml
//	go run ./example/common --plain
//	go run ./example/common --layers
//	go run ./example/common --sources=defaults          # show defaults layer
//	go run ./example/common --sources=-defaults,-flags  # hide defaults and flags
//	go run ./example/common --format=toml
//	go run ./example/common --db-url=postgres://prod/mydb
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
	kongcharming "github.com/pmarschik/kongfig/kong/charming"
	kongprovider "github.com/pmarschik/kongfig/kong/provider"
	kongshow "github.com/pmarschik/kongfig/kong/show"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	fileprovider "github.com/pmarschik/kongfig/providers/file"
	"github.com/pmarschik/kongfig/providers/file/discover"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/style/plain"
	"github.com/pmarschik/lipmark/theme"
)

type AppConfig struct {
	DB       DBConfig `kongfig:"db"`
	Host     string   `kongfig:"host"`
	LogLevel string   `kongfig:"log-level"`
	APIKey   string   `kongfig:"api-key,redacted"`
	Port     int      `kongfig:"port"`
}

type DBConfig struct {
	URL      string `kongfig:"url"`
	Password string `kongfig:"password,redacted"`
}

// DBCLI holds nested database flags. Embedding with prefix:"db-" makes kong expose
// --db-url; flagPath converts "db-url" → "db.url" so it maps to db.url in kongfig.
type DBCLI struct {
	URL string `name:"url" env:"APP_DB_URL" help:"Database URL."`
}

type CLI struct {
	Config   string         `name:"config"    short:"c"           config:"-"          optional:""                      type:"path"                 help:"Config file (yaml/toml/json/jsonc)." kongfig:",config-path"`
	Host     string         `name:"host"      env:"APP_HOST"      default:"localhost" help:"Server hostname."`
	LogLevel string         `name:"log-level" env:"APP_LOG_LEVEL" default:"info"      enum:"debug,info,warn,error"     help:"Log level (${enum})."`
	DB       DBCLI          `embed:""         prefix:"db-"`
	Show     kongshow.Flags `config:"-"       embed:""`
	Port     int            `name:"port"      short:"p"           env:"APP_PORT"      default:"8080"                   help:"Listen port."`
	Plain    bool           `name:"plain"     config:"-"          negatable:""        help:"Plain output (no colors)."`
}

var defaults = AppConfig{
	Host: "localhost", Port: 8080, LogLevel: "info",
	APIKey: "dev-placeholder",
	DB:     DBConfig{URL: "postgres://localhost/dev", Password: "dev-password"},
}

const appName = "common-example"

var parsers = []kongfig.Parser{yamlparser.Default, tomlparser.Default, jsonparser.WithComments, jsonparser.Default}

// setupKongfig creates and pre-populates the Kongfig instance before kong parses flags.
// Loads: defaults → XDG/workdir discovery → env.tag.
// bgCtx should have the app name set via [kongfig.WithAppName].
func setupKongfig(bgCtx context.Context) *kongfig.Kongfig {
	kf := kongfig.NewFor[AppConfig](kongfig.WithParsers(parsers...))
	kf.MustLoad(bgCtx, structsprovider.Defaults(defaults))

	// XDG + workdir discovery via the discover package — no hand-rolled path logic.
	// XDG is loaded first; workdir is loaded on top (last writer wins, so workdir
	// overrides XDG, which is the right behavior for local dev overrides).
	fileprovider.MustLoadAllDiscovered(bgCtx, kf, parsers, []fileprovider.Discoverer{discover.XDG(), discover.Workdir()})

	// Env overrides files (12-factor: defaults < file < env < flags).
	kf.MustLoad(bgCtx, structsprovider.TagEnv[CLI]())
	return kf
}

func main() {
	bgCtx := kongfig.WithAppName(context.Background(), appName)
	kf := setupKongfig(bgCtx)

	var cli CLI
	reg := theme.NewWithOptions(theme.WithDefaults())
	opts := []kong.Option{
		kongprovider.AppNameOption(bgCtx),
		kong.Description("kongfig common example: charming, XDG, env, file, layers, redacted.\n\nPass --config=example/common/config.yaml to load the sample config."),
		kong.UsageOnError(),
		kongshow.FlagsVarsFromKongfig(kf),
	}
	opts = append(opts, kongcharming.Options(kf, reg, "auto")...)

	k, err := kong.New(&cli, opts...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, err := k.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// env.kong intentionally overwrites env.tag (both are env layers); silence the expected collision.
	kf.MustLoad(bgCtx, kongprovider.Env(k), kongfig.WithSilenceCollisions())
	kf.MustLoad(bgCtx, kongprovider.Args(ctx))
	// Load any file referenced by a flag tagged kongfig-path (e.g. --config).
	kongprovider.MustLoadConfigPaths(bgCtx, k, kf)

	var styler kongfig.Styler
	if cli.Plain {
		styler = plain.New()
	} else {
		styler = kongcharming.Styler(reg, "auto")
	}

	if err := cli.Show.Render(bgCtx, os.Stdout, kf, styler); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
