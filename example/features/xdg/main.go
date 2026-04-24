// Package main: feature demo — XDG config file auto-discovery.
// Searches ~/.config/<app>/config.yaml, then ./config.yaml. No --config flag needed.
//
// Config search order:
//  1. $XDG_CONFIG_HOME/xdg-example/config.{yaml,toml}
//  2. ~/.config/xdg-example/config.{yaml,toml}
//  3. ./config.{yaml,toml}  (workdir fallback)
//
// Run:
//
//	go run ./example/features/xdg
//	XDG_CONFIG_HOME=/tmp go run ./example/features/xdg
package main

import (
	"context"
	"fmt"
	"os"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	fileprovider "github.com/pmarschik/kongfig/providers/file"
	"github.com/pmarschik/kongfig/providers/file/discover"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/style/plain"
)

type AppConfig struct {
	Host string `kongfig:"host"`
	Port int    `kongfig:"port"`
}

var defaults = AppConfig{Host: "localhost", Port: 8080}

const appName = "xdg-example"

var parsers = []kongfig.Parser{yamlparser.Default, tomlparser.Default}

func main() {
	ctx := kongfig.WithAppName(context.Background(), appName)
	kf := kongfig.New()
	kf.MustLoad(ctx, structsprovider.Defaults(defaults))

	// --- Feature: XDG config discovery via discover package ---
	// XDG is loaded first; workdir on top (local dev overrides XDG).
	fileprovider.MustLoadAllDiscovered(ctx, kf, parsers, []fileprovider.Discoverer{discover.XDG(), discover.Workdir()})
	// ---

	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New())); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
