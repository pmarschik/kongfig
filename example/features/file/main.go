// Package main: feature demo — file loading with parser auto-detection.
// Picks the right parser (YAML/TOML/JSON/JSONC) from the config file extension.
//
// Run:
//
//	go run ./example/features/file -config=example/features/file/config.yaml
//	go run ./example/features/file -config=example/features/file/config.toml
//	go run ./example/features/file -config=example/features/file/config.json
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	jsonparser "github.com/pmarschik/kongfig/parsers/json"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	fileprovider "github.com/pmarschik/kongfig/providers/file"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/style/plain"
)

type AppConfig struct {
	Host  string `kongfig:"host"`
	Port  int    `kongfig:"port"`
	Debug bool   `kongfig:"debug"`
}

var defaults = AppConfig{Host: "localhost", Port: 8080}

func main() {
	configPath := flag.String("config", "", "config file (.yaml/.toml/.json/.jsonc)")
	flag.Parse()

	ctx := context.Background()
	kf := kongfig.New()
	kf.MustLoad(ctx, structsprovider.Defaults(defaults))

	// --- Feature: auto-detect parser from file extension ---
	if *configPath != "" {
		parser, err := parserFor(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		kf.MustLoad(ctx, fileprovider.New(*configPath, parser), kongfig.WithSource("file"))
	}
	// ---

	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New())); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}

// parserFor returns the right parser for the given file path based on its extension.
func parserFor(path string) (kongfig.Parser, error) {
	switch {
	case strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml"):
		return yamlparser.Default, nil
	case strings.HasSuffix(path, ".toml"):
		return tomlparser.Default, nil
	case strings.HasSuffix(path, ".jsonc"):
		return jsonparser.WithComments, nil
	case strings.HasSuffix(path, ".json"):
		return jsonparser.Default, nil
	default:
		return nil, fmt.Errorf("unsupported config file extension: %s", path)
	}
}
