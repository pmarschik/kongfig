// Package file provides a file-based [kongfig.Provider] with optional watching.
package file

import (
	"context"
	"fmt"
	"os"

	"github.com/fsnotify/fsnotify"
	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
)

// Provider loads a config file using a Parser.
type Provider struct {
	parser      kongfig.Parser
	path        string
	source      string // optional override set by Discover; falls back to path
	displayPath string // optional human-readable path set by Discover (e.g. "$XDG_CONFIG_HOME/...")
}

// New returns a Provider (and ByteProvider) that loads the file at path using parser.
// If the file does not exist, Load returns an empty map (optional file semantics).
func New(path string, parser kongfig.Parser) *Provider {
	return &Provider{path: path, parser: parser}
}

// ProviderInfo returns the source label and kind.
// When created via Discover this is "file.<discoverer>.<format>".
// For providers created via New, name is "file" so manual and discovered
// providers share the same filterable prefix. Use WithSource at Load time
// to distinguish multiple manual file providers in the same Kongfig.
func (p *Provider) ProviderInfo() kongfig.ProviderInfo {
	name := p.source
	if name == "" {
		name = "file"
	}
	return kongfig.ProviderInfo{Name: name, Kind: kongfig.KindFile}
}

// SourceData carries file path information for source annotation rendering.
type SourceData struct {
	Path        string // canonical absolute path
	DisplayPath string // optional human-readable path (e.g. "$XDG_CONFIG_HOME/app/config.yaml")
}

// RenderAnnotation returns the display path when available, otherwise the raw path.
// Set [kongfig.WithRenderFileRawPaths] (or check [kongfig.RenderFileRawPaths]) to
// always use the raw canonical path.
func (d SourceData) RenderAnnotation(ctx context.Context, s kongfig.Styler, _ string) string {
	path := d.Path
	if d.DisplayPath != "" && !render.FileRawPaths(ctx) {
		path = d.DisplayPath
	}
	return s.SourceData(path)
}

// ProviderData implements [kongfig.ProviderDataSupport].
// Returns nil for empty no-op providers (no path).
func (p *Provider) ProviderData() kongfig.ProviderData {
	if p.path == "" {
		return nil
	}
	return SourceData{Path: p.path, DisplayPath: p.displayPath}
}

// Parser returns the parser used by this provider. Implements [kongfig.ParserProvider]
// so that [kongfig.Kongfig] can record the parser on the loaded [kongfig.Layer] for
// native format rendering in --layers mode.
func (p *Provider) Parser() kongfig.Parser { return p.parser }

// LoadBytes returns the raw file contents.
func (p *Provider) LoadBytes(_ context.Context) ([]byte, error) {
	b, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("file provider: read %s: %w", p.path, err)
	}
	return b, nil
}

// Load parses the file and returns the config map.
func (p *Provider) Load(ctx context.Context) (kongfig.ConfigData, error) {
	b, err := p.LoadBytes(ctx)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return kongfig.ConfigData{}, nil
	}
	data, err := p.parser.Unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("file provider: parse %s: %w", p.path, err)
	}
	return data, nil
}

// Bind returns a Renderer if the parser implements OutputProvider; otherwise nil.
func (p *Provider) Bind(s kongfig.Styler) kongfig.Renderer {
	if op, ok := p.parser.(kongfig.OutputProvider); ok {
		return op.Bind(s)
	}
	return nil
}

// Watcher wraps Provider and implements WatchProvider.
type Watcher struct {
	Provider
}

// NewWatcher returns a WatchProvider that reloads the file on change.
// Watch blocks until ctx is canceled.
func NewWatcher(path string, parser kongfig.Parser) *Watcher {
	return &Watcher{Provider: Provider{path: path, parser: parser}}
}

// Watch starts watching the file and calls cb on each change.
func (w *Watcher) Watch(ctx context.Context, cb kongfig.WatchFunc) (retErr error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("file watcher: %w", err)
	}
	defer func() {
		if cerr := fsw.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	if err := fsw.Add(w.path); err != nil {
		return fmt.Errorf("file watcher: watch %s: %w", w.path, err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				data, loadErr := w.Load(ctx)
				if loadErr != nil {
					cb(kongfig.WatchErrorEvent{Err: loadErr})
				} else {
					cb(kongfig.WatchDataEvent{Data: data})
				}
			}
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			cb(kongfig.WatchErrorEvent{Err: err})
		}
	}
}
