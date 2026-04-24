// Package main: feature demo — env var provider via struct tags.
// Fields with env:"VAR" tags are loaded from os.Environ; source shows as "env.tag".
//
// Run:
//
//	go run ./example/features/struct-env
//	ENV_HOST=prod.example.com ENV_PORT=9090 ENV_DEBUG=true go run ./example/features/struct-env
package main

import (
	"context"
	"fmt"
	"os"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/style/plain"
)

type AppConfig struct {
	Host  string `kongfig:"host"`
	Port  int    `kongfig:"port"`
	Debug bool   `kongfig:"debug"`
}

// --- Feature: env:"VAR" maps each env var to a kongfig path ---
// TagEnv[Env]() reads os.Environ for any declared vars and loads them.
// Source label is "env.tag"; render shows "# env" per value.
type Env struct {
	Host  string `env:"ENV_HOST"  kongfig:"host"`
	Port  int    `env:"ENV_PORT"  kongfig:"port"`
	Debug bool   `env:"ENV_DEBUG" kongfig:"debug"`
}

// ---

var defaults = AppConfig{Host: "localhost", Port: 8080}

func main() {
	ctx := context.Background()
	kf := kongfig.New()
	kf.MustLoad(ctx, structsprovider.Defaults(defaults))

	// --- Feature: load env vars declared in Env struct ---
	kf.MustLoad(ctx, structsprovider.TagEnv[Env]())
	// ---

	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New())); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
