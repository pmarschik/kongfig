// Package main: feature demo — redacted values in rendered output.
// Fields tagged kongfig:",redacted" are replaced with "<redacted>" in render.
// Redaction is inherited by nested structs; individual fields can opt out.
//
// Run:
//
//	go run ./example/features/redacted
//	API_KEY=mysecret go run ./example/features/redacted
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

// --- Feature: struct tag conventions for redaction ---

// DBConfig demonstrates redaction inheritance: the parent is marked redacted,
// so all leaf fields inherit it. Individual fields can opt out with redacted=false.
type DBConfig struct {
	Host     string `kongfig:"host,redacted=false"` // opt-out: shown in plain text
	Password string `kongfig:"password"`            // inherits parent redaction → hidden
}

// AppConfig mixes plain, single-field redacted, and sub-tree redacted.
type AppConfig struct {
	DB     DBConfig `kongfig:"db,redacted"`
	Host   string   `kongfig:"host"`
	APIKey string   `kongfig:"api-key,redacted"`
	Port   int      `kongfig:"port"`
}

// ---

type Env struct {
	APIKey string `env:"API_KEY" kongfig:"api-key"`
}

var defaults = AppConfig{
	Host:   "localhost",
	Port:   8080,
	APIKey: "dev-placeholder",
	DB:     DBConfig{Host: "db.local", Password: "dev-password"},
}

func main() {
	// --- Feature: register redacted paths at construction time ---
	ctx := context.Background()
	kf := kongfig.NewFor[AppConfig]()
	// ---
	kf.MustLoad(ctx, structsprovider.Defaults(defaults))
	kf.MustLoad(ctx, structsprovider.TagEnv[Env]())

	// --- Feature: RenderWith prepares leaves (applying RenderConfig redaction) and renders ---
	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New())); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
