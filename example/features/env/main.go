// Package main: feature demo — prefix-based env var provider.
// All env vars with the APP_ prefix are loaded; the prefix+separator is stripped and
// underscores are converted to dots to form nested key paths.
// E.g. APP_DB_HOST → db.host  (prefix="APP", separator="_", lowercased after strip).
//
// Run:
//
//	go run ./example/features/env
//	APP_HOST=prod.example.com APP_PORT=9090 APP_DEBUG=true go run ./example/features/env
//	APP_DB_HOST=db.prod APP_DB_PORT=5432 go run ./example/features/env
package main

import (
	"context"
	"fmt"
	"os"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	envprovider "github.com/pmarschik/kongfig/providers/env"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/style/plain"
)

type AppConfig struct {
	Host  string   `kongfig:"host"`
	DB    DBConfig `kongfig:"db"`
	Port  int      `kongfig:"port"`
	Debug bool     `kongfig:"debug"`
}

type DBConfig struct {
	Host string `kongfig:"host"`
	Port int    `kongfig:"port"`
}

var defaults = AppConfig{Host: "localhost", Port: 8080, DB: DBConfig{Host: "db.local", Port: 5432}}

func main() {
	ctx := context.Background()
	kf := kongfig.New()
	kf.MustLoad(ctx, structsprovider.Defaults(defaults))

	// --- Feature: load all APP_* env vars; strip prefix+separator, split on _ → dot paths ---
	// No struct declaration needed — any APP_XYZ var maps to xyz, APP_DB_HOST → db.host.
	kf.MustLoad(ctx, envprovider.Provider("APP", "_"))
	// ---

	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New())); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
