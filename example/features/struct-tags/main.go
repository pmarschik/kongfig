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
// NewFor[Config] reflects on these tags (via schema.HelpTextPaths) and registers
// the descriptions, so rendered output carries them with no extra wiring.
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

	// NewFor already reflected on the help= tags and registered the texts, so
	// each description is emitted as a comment above its key — at most once per
	// render call — without any WithRenderHelpTexts call. Pass
	// kongfig.WithRenderHelpTexts(schema.HelpTextPaths[Config]()) to override the
	// derived set for a single render.
	if err := kf.RenderWith(ctx, os.Stdout, yamlparser.Default.Bind(plain.New())); err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
}
