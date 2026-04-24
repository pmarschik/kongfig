// Package main: feature demo — kongfig struct tag conventions.
// Shows every kongfig tag variant: explicit name, empty name, skip, redacted,
// redacted inheritance, redacted=false opt-out, and nested struct namespacing.
//
// Run:
//
//	go run ./example/features/struct-tags
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

// --- Feature: kongfig struct tag conventions ---

// DBConfig shows redaction inheritance with selective opt-out.
type DBConfig struct {
	Host     string `kongfig:"host,redacted=false"` // parent is redacted; this opts out
	Password string `kongfig:"password"`            // inherits parent redaction → hidden
	Port     int    `kongfig:"port,redacted=false"` // opt-out: shown in plain text
}

// Config demonstrates all tag variants.
type Config struct {
	Host     string   `kongfig:"host"`
	APIKey   string   `kongfig:"api-key,redacted"`
	Internal string   `kongfig:"-"`
	DB       DBConfig `kongfig:"db,redacted"`
	Port     int      `kongfig:""`
	Debug    bool     `kongfig:"debug"`
}

// ---

var defaults = Config{
	Host:     "localhost",
	Port:     8080,
	APIKey:   "dev-placeholder",
	Internal: "ignored",
	DB:       DBConfig{Host: "db.local", Password: "dev-password", Port: 5432},
}

func main() {
	ctx := context.Background()
	kf := kongfig.NewFor[Config]()
	kf.MustLoad(ctx, structsprovider.Defaults(defaults))

	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New())); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
