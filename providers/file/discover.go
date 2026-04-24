package file

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
)

// Discoverer finds a config file given a set of supported file extensions.
// Implementations should return ("", nil) when no file is found.
type Discoverer interface {
	// Name returns the discovery method label used as the first source segment,
	// e.g. "xdg", "workdir", "git-root". Combined with the parser format this
	// produces source labels like "xdg.yaml".
	Name() string
	// Discover searches for a config file matching one of the given extensions.
	// ctx may carry values injected by integrations (e.g. app name via discover.WithAppName).
	// Returns the found path, or ("", nil) if nothing was found.
	Discover(ctx context.Context, exts []string) (string, error)
}

// Discover returns a Provider that uses d to find a config file parsed by the
// first matching parser. The source label is "<d.Name()>.<format>".
// Returns (empty-provider, nil) when no file is found; returns (nil, err) when the
// discoverer fails so callers can distinguish "not found" from "failed to discover".
func Discover(ctx context.Context, d Discoverer, parsers ...kongfig.Parser) (*Provider, error) {
	for _, p := range parsers {
		exts := parserExtensions(p)
		path, err := d.Discover(ctx, exts)
		if err != nil {
			return nil, fmt.Errorf("discoverer %q failed: %w", d.Name(), err)
		}
		if path == "" {
			continue
		}
		prov := New(path, p)
		prov.source = buildDiscoveredSource(d.Name(), path, p)
		if dp, ok := d.(interface {
			DisplayPath(context.Context, string) string
		}); ok {
			prov.displayPath = dp.DisplayPath(ctx, path)
		}
		return prov, nil
	}
	return &Provider{source: d.Name()}, nil // no-op empty provider
}

// DiscoverAll returns a Provider for each Discoverer, each with its own source label.
// Returns an error if any discoverer fails; callers must handle the error.
// Providers for discoverers that find no file load empty maps (no error).
func DiscoverAll(ctx context.Context, parsers []kongfig.Parser, discoverers ...Discoverer) ([]*Provider, error) {
	out := make([]*Provider, 0, len(discoverers))
	for _, d := range discoverers {
		prov, err := Discover(ctx, d, parsers...)
		if err != nil {
			return nil, err
		}
		out = append(out, prov)
	}
	return out, nil
}

// MustLoadDiscovered discovers a config file using d and immediately loads it into k.
// Panics on any error (discover failure or load failure). Passes opts to Load.
// This is the single-call convenience wrapper for the common pattern:
//
//	p, err := file.Discover(ctx, d, parser)
//	if err != nil { panic(err) }
//	k.MustLoad(ctx, p)
func MustLoadDiscovered(ctx context.Context, k *kongfig.Kongfig, d Discoverer, parsers []kongfig.Parser, opts ...kongfig.LoadOption) {
	p, err := Discover(ctx, d, parsers...)
	if err != nil {
		panic(err)
	}
	k.MustLoad(ctx, p, opts...)
}

// MustLoadAllDiscovered discovers and loads a config file for each Discoverer in order.
// Panics on any error. Passes opts to each Load call.
func MustLoadAllDiscovered(ctx context.Context, k *kongfig.Kongfig, parsers []kongfig.Parser, discoverers []Discoverer, opts ...kongfig.LoadOption) {
	providers, err := DiscoverAll(ctx, parsers, discoverers...)
	if err != nil {
		panic(err)
	}
	kongfig.MustLoadAll(ctx, k, providers, opts...)
}

// buildDiscoveredSource composes "<name>.<format>" using the parser's Format()
// if it implements [kongfig.ParserNamer]; falls back to extension inference.
func buildDiscoveredSource(name, path string, p kongfig.Parser) string {
	if namer, ok := p.(kongfig.ParserNamer); ok {
		return fmt.Sprintf("%s.%s", name, namer.Format())
	}
	return fmt.Sprintf("%s.%s", name, extFormat(path))
}

// parserExtensions returns extensions from ParserNamer; nil if not implemented.
func parserExtensions(p kongfig.Parser) []string {
	if namer, ok := p.(kongfig.ParserNamer); ok {
		return namer.Extensions()
	}
	return nil
}

// extFormat infers a format name from a file path extension.
func extFormat(path string) string {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext == "yml" {
		return "yaml"
	}
	return ext
}
