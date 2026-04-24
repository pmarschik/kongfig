// Package main: minimal kongfig example using stdlib flag — no kong dependency.
// Shows the three core layers: defaults → env → file, rendered as plain YAML.
//
// Run:
//
//	go run ./example/minimal
//	go run ./example/minimal -config=example/minimal/config.yaml
//	MINIMAL_HOST=prod.example.com go run ./example/minimal
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	fileprovider "github.com/pmarschik/kongfig/providers/file"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/style/plain"
)

// AppConfig is the target type for kongfig.Get.
type AppConfig struct {
	Host string `kongfig:"host"`
	Port int    `kongfig:"port"`
}

// Env maps env var names to kongfig paths via struct tags.
type Env struct {
	Host string `env:"MINIMAL_HOST" kongfig:"host"`
	Port int    `env:"MINIMAL_PORT" kongfig:"port"`
}

var defaults = AppConfig{Host: "localhost", Port: 8080}

func main() {
	configPath := flag.String("config", "", "path to YAML config file")
	flag.Parse()

	ctx := context.Background()
	kf := kongfig.New()
	kf.MustLoad(ctx, structsprovider.Defaults(defaults))
	if *configPath != "" {
		kf.MustLoad(ctx, fileprovider.New(*configPath, yamlparser.Default), kongfig.WithSource("file"))
	}
	// Env overrides files (12-factor: defaults < file < env).
	kf.MustLoad(ctx, structsprovider.TagEnv[Env]())

	cfg, err := kongfig.Get[AppConfig](kf)
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}
	_ = cfg

	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New())); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
