// Package main: full kongfig example — deep customization.
// Everything in common, plus: custom per-layer and config colors,
// struct-tag showcase, all parsers, and fully wired RenderOptions.
//
// Run:
//
//	go run ./example/full
//	go run ./example/full --config=example/full/config.yaml
//	go run ./example/full --config=example/full/config.toml
//	go run ./example/full --plain
//	go run ./example/full --layers
//	go run ./example/full --format=env
//	go run ./example/full --defaults
//	go run ./example/full --no-align    # disable source annotation column alignment
//	go run ./example/full --help
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
	render "github.com/pmarschik/kongfig/render"
	"github.com/pmarschik/kongfig/style/plain"
	"github.com/pmarschik/kongfig/validation"
	"github.com/pmarschik/lipmark/theme"
)

// --- Struct-tag showcase: all kongfig tag variants in production config ---

// DBConfig: nested namespace + redaction inheritance with opt-out.
type DBConfig struct {
	Host     string `kongfig:"host,redacted=false"`
	Password string `kongfig:"password"`
	Port     int    `kongfig:"port,redacted=false"`
}

// AppConfig: full set of tag variants.
type AppConfig struct {
	Host     string   `kongfig:"host"`
	LogLevel string   `kongfig:"log-level"`
	APIKey   string   `kongfig:"api-key,redacted"`
	DB       DBConfig `kongfig:"db,redacted"`
	Port     int      `kongfig:""`
}

// ---

type CLI struct {
	Config       string         `name:"config"    short:"c"            config:"-"          optional:""                      type:"path"                                                                 help:"Config file (yaml/toml/json/jsonc)." kongfig:",config-path"`
	Host         string         `name:"host"      env:"FULL_HOST"      default:"localhost" help:"Server hostname."`
	LogLevel     string         `name:"log-level" env:"FULL_LOG_LEVEL" default:"info"      enum:"debug,info,warn,error"     help:"Log level (${enum})."`
	Show         kongshow.Flags `config:"-"       embed:""`
	Port         int            `name:"port"      short:"p"            env:"FULL_PORT"     default:"8080"                   help:"Listen port."`
	Plain        bool           `name:"plain"     config:"-"           negatable:""        help:"Plain output (no colors)."`
	Align        bool           `name:"align"     config:"-"           default:"true"      negatable:""                     help:"Align source annotations to the same column (--no-align to disable)."`
	ShowDefaults bool           `name:"defaults"  config:"-"           default:"false"     negatable:""                     help:"Show defaults layer."`
	ShowEnv      bool           `name:"env"       config:"-"           default:"true"      negatable:""                     help:"Show env layer."`
	ShowFile     bool           `name:"file"      config:"-"           default:"true"      negatable:""                     help:"Show file/xdg/workdir layers."`
	ShowFlags    bool           `name:"flags"     config:"-"           default:"true"      negatable:""                     help:"Show flags layer."`
}

var defaults = AppConfig{
	Host: "localhost", Port: 8080, LogLevel: "info",
	APIKey: "dev-placeholder",
	DB:     DBConfig{Host: "db.local", Port: 5432, Password: "dev-password"},
}

const appName = "full-example"

var parsers = []kongfig.Parser{yamlparser.Default, tomlparser.Default, jsonparser.WithComments, jsonparser.Default}

// setupKongfig creates and pre-populates the Kongfig instance before kong parses flags.
// Loads: defaults → XDG/workdir discovery → env.tag.
// bgCtx should have the app name set via [kongfig.WithAppName].
func setupKongfig(bgCtx context.Context) *kongfig.Kongfig {
	kf := kongfig.NewFor[AppConfig]()
	kf.RegisterParsers(parsers...)
	kf.MustLoad(bgCtx, structsprovider.Defaults(defaults))

	// XDG + workdir discovery via discover package — no hand-rolled path logic.
	fileprovider.MustLoadAllDiscovered(bgCtx, kf, parsers, []fileprovider.Discoverer{discover.XDG(), discover.Workdir()})

	// Env overrides files (12-factor: defaults < file < env < flags).
	kf.MustLoad(bgCtx, structsprovider.TagEnv[CLI]())
	return kf
}

func main() {
	bgCtx := kongfig.WithAppName(context.Background(), appName)
	kf := setupKongfig(bgCtx)

	var cli CLI

	// --- Custom per-layer colors and config value colors ---
	reg := theme.NewWithOptions(theme.WithDefaults())
	reg.RegisterStruct("auto", kongcharming.ConfigStyleDefs{
		Derived: theme.StyleDef{Foreground: "#e0af68"}, // amber for derived values
	})
	reg.RegisterStruct("auto", kongcharming.LayerStyleDefs{
		Flags:    theme.StyleDef{Foreground: "#9ece6a", Bold: true}, // green bold
		Env:      theme.StyleDef{Foreground: "#7dcfff"},             // sky blue
		File:     theme.StyleDef{Foreground: "#bb9af7"},             // purple
		XDG:      theme.StyleDef{Foreground: "#bb9af7"},             // purple (same as file)
		Workdir:  theme.StyleDef{Foreground: "#bb9af7"},             // purple
		Defaults: theme.StyleDef{Foreground: "#565f89"},             // muted blue-gray
	})
	// ---

	opts := []kong.Option{
		kongprovider.AppNameOption(bgCtx),
		kong.Description("kongfig full example: deep customization.\n\nPass --config=example/full/config.yaml (or .toml) to load the sample config."),
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

	// --- Validation ---
	if exit := validateConfig(kf); exit != 0 {
		os.Exit(exit)
	}
	// ---

	var styler kongfig.Styler
	if cli.Plain {
		styler = plain.New()
	} else {
		styler = kongcharming.Styler(reg, "auto")
	}

	renderOpts := []kongfig.RenderOption{
		// --- Fine-grained render options ---
		kongfig.WithRenderFilterSource(render.BuildFilterSource(map[string]bool{
			"defaults": cli.ShowDefaults,
			"env":      cli.ShowEnv,
			"file":     cli.ShowFile,
			"xdg":      cli.ShowFile,
			"workdir":  cli.ShowFile,
			"flags":    cli.ShowFlags,
		})),
		// ---
	}
	if !cli.Align {
		renderOpts = append(renderOpts, kongfig.WithRenderNoAlignSources())
	}
	if err := cli.Show.Render(bgCtx, os.Stdout, kf, styler, renderOpts...); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}

// --- Validator functions for validateConfig ---

func validatePort(e validation.Event) []validation.FieldViolation {
	n, ok := e.Value.(int)
	if !ok || (n >= 1 && n <= 65535) {
		return nil
	}
	return []validation.FieldViolation{{Message: fmt.Sprintf("port %d out of range 1–65535", n), Code: "port.range"}}
}

func validateLogLevel(e validation.Event) []validation.FieldViolation {
	s, ok := e.Value.(string)
	if !ok || s != "" {
		return nil
	}
	return []validation.FieldViolation{{Message: "log-level must not be empty", Code: "log-level.required", Severity: validation.SeverityWarning}}
}

// dbHostRule is a flat projection for the db.host presence rule.
type dbHostRule struct {
	Host string `kongfig:"db.host"`
}

func validateDBHost(db dbHostRule) []validation.FieldViolation {
	if db.Host == "" {
		return []validation.FieldViolation{{Message: "db.host is required", Code: "db.host.required"}}
	}
	return nil
}

func validateConfig(kf *kongfig.Kongfig) int {
	v := validation.NewWithDefaults()
	v.AddValidator("port", validatePort)
	v.AddValidator("log-level", validateLogLevel)
	v.AddRule(validation.Rule(validateDBHost))
	d := v.Validate(kf)
	if d == nil {
		return 0
	}
	for _, viol := range d.Violations {
		fmt.Fprintf(os.Stderr, "config %s: %s (%s)\n", viol.Severity, viol.Message, viol.Code)
	}
	if err := d.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "config invalid:", err)
		return 1
	}
	return 0
}

// ---
