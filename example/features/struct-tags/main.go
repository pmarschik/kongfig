// Package main: feature demo — kongfig struct tag conventions.
// Shows every kongfig tag variant: explicit name, empty name, skip, redacted,
// redacted inheritance, redacted=false opt-out, nested struct namespacing,
// default= inline defaults, and help= inline documentation.
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
	"github.com/pmarschik/kongfig/schema"
	"github.com/pmarschik/kongfig/style/plain"
)

// --- Feature: kongfig struct tag conventions ---

// DBConfig shows redaction inheritance with selective opt-out.
type DBConfig struct {
	Host     string `kongfig:"host,redacted=false"`                             // parent is redacted; this opts out
	Password string `kongfig:"password"`                                        // inherits parent redaction → hidden
	Port     int    `kongfig:"port,redacted=false,help='database port number'"` // opt-out + inline doc
}

// Config demonstrates all tag variants including help= inline documentation.
// schema.HelpTextPaths[Config]() reflects on these tags and returns a
// map[string]string that can be passed to WithRenderHelpTexts.
type Config struct {
	Host     string   `kongfig:"host,default=localhost,help='hostname or IP to listen on'"`
	APIKey   string   `kongfig:"api-key,redacted"`
	Internal string   `kongfig:"-"`
	DB       DBConfig `kongfig:"db,redacted"`
	Port     int      `kongfig:",default=8080,help='TCP port'"`
	Debug    bool     `kongfig:"debug,help='enable verbose debug logging'"`
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

	// schema.HelpTextPaths reflects on the struct tags and builds the help map.
	// WithRenderHelpTexts emits each description as a comment above its key,
	// at most once per render call.
	helpTexts := schema.HelpTextPaths[Config]()

	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New()),
		kongfig.WithRenderHelpTexts(helpTexts),
	); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
