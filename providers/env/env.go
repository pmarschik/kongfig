// Package env provides environment-variable [kongfig.Provider] implementations.
package env

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"sort"
	"strings"
	"sync"

	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
	"github.com/pmarschik/kongfig/schema"
)

// loaderProviderData is the ProviderData implementation shared by all Loader variants.
// Load() writes loaded names into it; FieldNames() exposes them to the framework,
// which injects them into the render context so RenderAnnotation can read them via FieldNameFromCtx.
type loaderProviderData struct {
	names map[string]string // dotPath → envKey; written by Load()
	mu    sync.RWMutex
}

// RenderAnnotation returns the env var name for the given path, styled as a source key.
// Reads from context PathFieldNames set by the framework via FieldNames().
func (*loaderProviderData) RenderAnnotation(ctx context.Context, s kongfig.Styler, path string) string {
	if name := render.FieldName(ctx, path); name != "" {
		return s.SourceKey("$" + name)
	}
	return ""
}

func (d *loaderProviderData) loadedNames() map[string]string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.names) == 0 {
		return nil
	}
	out := make(map[string]string, len(d.names))
	maps.Copy(out, d.names)
	return out
}

// Loader loads config from environment variables.
// All mapping logic is captured in mapFn at construction time.
type Loader struct {
	mapFn func(string) string // full-key → dot-path, "" to skip
	pdata *loaderProviderData // shared with LayerMeta; Load() writes names here
	name  string
}

// Provider returns a Loader that reads env vars with the given prefix,
// strips the prefix+delimiter, and splits the remainder by delimiter to form nested key paths.
// E.g. prefix="APP", delimiter="_": APP_DB_HOST → db.host (lowercased).
// An empty prefix matches all env vars.
func Provider(prefix, delimiter string) *Loader {
	matchPfx := prefix
	if prefix != "" && delimiter != "" {
		matchPfx = prefix + delimiter
	}
	pdata := &loaderProviderData{}
	return &Loader{
		mapFn: prefixMapFn(matchPfx, delimiter),
		pdata: pdata,
		name:  "env.prefix",
	}
}

// prefixMapFn returns a full-key mapper for the given match-prefix and delimiter.
func prefixMapFn(matchPfx, delimiter string) func(string) string {
	return func(k string) string {
		if !strings.HasPrefix(k, matchPfx) {
			return ""
		}
		stripped := k[len(matchPfx):]
		if stripped == "" {
			return ""
		}
		parts := strings.Split(stripped, delimiter)
		for i, p := range parts {
			parts[i] = strings.ToLower(p)
		}
		return strings.Join(parts, ".")
	}
}

// ProviderWithCallback returns a Loader that applies cb to each stripped key.
func ProviderWithCallback(prefix string, cb func(string) string) *Loader {
	mapFn := func(k string) string {
		if !strings.HasPrefix(k, prefix) {
			return ""
		}
		stripped := k[len(prefix):]
		if stripped == "" {
			return ""
		}
		return cb(stripped)
	}
	return &Loader{
		mapFn: mapFn,
		pdata: &loaderProviderData{},
		name:  "env.prefix",
	}
}

// ProviderWithKeyFunc returns a Loader that calls fn with each full env var key.
// fn should return the dot-path for the key (e.g. "db.host"), or "" to skip it.
// Unlike Provider and ProviderWithCallback, no prefix stripping is performed;
// fn receives the raw key (e.g. "APP_DB_HOST") and is responsible for all mapping logic.
func ProviderWithKeyFunc(fn func(envKey string) string) *Loader {
	return &Loader{
		mapFn: fn,
		pdata: &loaderProviderData{},
		name:  "env.keyfunc",
	}
}

// ProviderData carries env layer metadata for source annotation rendering.
// Used by custom env providers and env.kong that don't use [Loader] directly.
type ProviderData struct{}

// RenderAnnotation returns the env var name for path (e.g. "$APP_DB_HOST"),
// or "" if no mapping is found in ctx (env var names hidden or not registered).
func (ProviderData) RenderAnnotation(ctx context.Context, s kongfig.Styler, path string) string {
	if name := render.FieldName(ctx, path); name != "" {
		return s.SourceKey("$" + name)
	}
	return ""
}

// ProviderData implements [kongfig.ProviderDataSupport].
func (p *Loader) ProviderData() kongfig.ProviderData { return p.pdata }

// FieldNames implements [kongfig.ProviderFieldNamesSupport].
// Returns the dotPath → envVarName mapping captured during the last Load() call.
func (p *Loader) FieldNames() map[string]string { return p.pdata.loadedNames() }

// ProviderInfo returns the source label and kind.
func (p *Loader) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: p.name, Kind: kongfig.KindEnv}
}

// Load reads matching env vars and returns a nested map.
// Also writes the dotPath → envVarName mapping into pdata for FieldNames and RenderAnnotation.
func (p *Loader) Load(_ context.Context) (kongfig.ConfigData, error) {
	out := make(kongfig.ConfigData)
	names := make(map[string]string)
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		dotPath := p.mapFn(k)
		if dotPath == "" {
			continue
		}
		setNestedStr(out, strings.Split(dotPath, "."), v)
		names[dotPath] = k
	}
	p.pdata.mu.Lock()
	p.pdata.names = names
	p.pdata.mu.Unlock()
	return out, nil
}

// Bind returns a Renderer that writes export KEY=value lines.
func (*Loader) Bind(s kongfig.Styler) kongfig.Renderer {
	return &envRenderer{s: s}
}

func setNestedStr(m kongfig.ConfigData, parts []string, val string) {
	if len(parts) == 1 {
		m[parts[0]] = val
		return
	}
	sub, ok := m[parts[0]].(kongfig.ConfigData)
	if !ok {
		sub = make(kongfig.ConfigData)
		m[parts[0]] = sub
	}
	setNestedStr(sub, parts[1:], val)
}

// envRenderer writes config as shell export statements.
type envRenderer struct {
	s      kongfig.Styler
	prefix string
}

func (r *envRenderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
	if !render.AlignSources(ctx) {
		return renderEnvMap(ctx, w, r.s, data, r.prefix, "", false)
	}
	var buf bytes.Buffer
	if err := renderEnvMap(ctx, &buf, r.s, data, r.prefix, "", true); err != nil {
		return err
	}
	return render.AlignAnnotations(buf.String(), w)
}

// envKeyResult describes how a key should be rendered.
type envKeyResult int

const (
	envKeyUse     envKeyResult = iota // use the resolved key
	envKeyRecurse                     // unbound parent map: recurse without filter
	envKeyComment                     // unbound leaf: render as comment
)

// resolveEnvKey resolves the env var key for path/v from the field names in ctx.
// sid is the SourceID of the rendered value (from RenderedValue.Source.Layer.ID); 0 = unknown.
// Returns the effective key and a render directive.
func resolveEnvKey(ctx context.Context, path, generated string, v any, sid kongfig.SourceID) (key string, result envKeyResult) {
	fieldNames := render.FieldNames(ctx)
	if fieldNames == nil {
		return generated, envKeyUse
	}
	if names, ok := fieldNames[path]; ok {
		if sid != 0 {
			if actual := names[sid]; actual != "" {
				return actual, envKeyUse
			}
		}
	}
	if _, isSub := v.(kongfig.ConfigData); isSub {
		return generated, envKeyRecurse
	}
	return generated, envKeyComment
}

//nolint:gocognit,cyclop // complex recursive renderer, intentional
func renderEnvMap(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, envPrefix, pathPrefix string, align bool) error {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	noComments := render.NoComments(ctx)

	for _, k := range keys {
		v := data[k]
		path := k
		if pathPrefix != "" {
			path = pathPrefix + "." + k
		}
		generated := envPrefix + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(k))

		// Unwrap RenderedValue to get actual leaf value and source info.
		rv, isRV := v.(kongfig.RenderedValue)
		var leafVal any
		if isRV {
			leafVal = rv.Value
		} else {
			leafVal = v
		}

		var sid kongfig.SourceID
		if isRV {
			sid = rv.Source.Layer.ID
		}
		envKey, result := resolveEnvKey(ctx, path, generated, leafVal, sid)

		// Unbound parent map: recurse directly, skipping the source filter
		// (children carry their own provenance).
		if result == envKeyRecurse {
			if sub, ok := leafVal.(kongfig.ConfigData); ok {
				if err := renderEnvMap(ctx, w, s, sub, envKey+"_", path, align); err != nil {
					return err
				}
			}
			continue
		}

		// Unbound leaf: render as comment after filter check.
		if result == envKeyComment {
			fmt.Fprintln(w, s.Comment("# "+path+"="+fmt.Sprintf("%v", leafVal)+"  # (no env binding)"))
			continue
		}

		// Bound sub-map (env var names has an explicit key for this path).
		if sub, ok := leafVal.(kongfig.ConfigData); ok {
			if err := renderEnvMap(ctx, w, s, sub, envKey+"_", path, align); err != nil {
				return err
			}
			continue
		}

		var line string
		if isRV && rv.Redacted {
			line = "export " + s.Key(envKey) + "=" + s.Redacted(rv.RedactedDisplay)
		} else {
			line = "export " + s.Key(envKey) + "=" + renderLeafValue(ctx, s, leafVal, path)
		}
		if !noComments && isRV {
			if ann := render.Annotation(ctx, rv, path, s); ann != "" {
				if align {
					line += render.AnnMarker + "  " + s.Comment("# ") + ann
				} else {
					line += "  " + s.Comment("# ") + ann
				}
			}
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// renderLeafValue formats a leaf config value for env output using a type switch.
func renderLeafValue(ctx context.Context, s kongfig.Styler, v any, path string) string {
	switch lv := v.(type) {
	case []string:
		sep := ","
		if registered, ok := kongfig.SplitSepKey.Get(ctx, path); ok {
			sep = registered
		}
		return s.String(strings.Join(lv, sep))
	case map[string]string:
		return s.String(renderMapValue(ctx, lv, path))
	default:
		return render.Value(s, v, fmt.Sprintf("%q", fmt.Sprintf("%v", v)))
	}
}

// renderMapValue serializes m as a delimited key=value string using the
// registered [schema.MapSplitSpec] for path (default keysep=",", sep="=").
// Map keys are sorted for deterministic output.
func renderMapValue(ctx context.Context, m map[string]string, path string) string {
	spec := schema.MapSplitSpec{Sep: ",", KVSep: "="}
	if registered, ok := kongfig.MapSplitSpecKey.Get(ctx, path); ok {
		spec = registered
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(m))
	for _, k := range keys {
		pairs = append(pairs, k+spec.KVSep+m[k])
	}
	return strings.Join(pairs, spec.Sep)
}
