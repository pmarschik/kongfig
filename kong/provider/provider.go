// Package provider implements [kongfig.Provider] for kong CLI flags.
// Three providers cover the three config layers kong contributes:
// defaults, environment variables, and explicit CLI arguments.
package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
	envprovider "github.com/pmarschik/kongfig/providers/env"
	fileprovider "github.com/pmarschik/kongfig/providers/file"
	render "github.com/pmarschik/kongfig/render"
	"github.com/pmarschik/kongfig/schema"
)

// staticProvider holds a pre-built map and source label.
type staticProvider struct {
	data         kongfig.ConfigData
	providerData kongfig.ProviderData
	fieldNames   map[string]string
	source       string
	kind         string
}

func (p *staticProvider) Load(_ context.Context) (kongfig.ConfigData, error) { return p.data, nil }
func (p *staticProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: p.source, Kind: p.kind}
}
func (p *staticProvider) ProviderData() kongfig.ProviderData { return p.providerData }
func (p *staticProvider) FieldNames() map[string]string      { return p.fieldNames }

// flagsProviderData implements [kongfig.ProviderData] for the "flags" source.
// It reads the flag name from [kongfig.RenderFieldNames] via the source ID injected
// by [kongfig.LayerMeta.RenderAnnotation].
type flagsProviderData struct{}

func (flagsProviderData) RenderAnnotation(ctx context.Context, s kongfig.Styler, path string) string {
	if name := render.FieldName(ctx, path); name != "" {
		return s.SourceKey(name)
	}
	return ""
}

// Defaults returns a Provider containing the default values declared in kong struct tags.
// Source label is "defaults".
func Defaults(k *kong.Kong) kongfig.Provider {
	data := make(kongfig.ConfigData)
	for _, flag := range allFlags(k) {
		if flag.Default == "" {
			continue
		}
		path := flagPath(flag)
		setNested(data, strings.Split(path, "."), parseNative(flag.Target, flag.Default))
	}
	return &staticProvider{data: data, source: "defaults", kind: kongfig.KindDefaults}
}

// Env returns a Provider containing values that kong read from environment variables.
// Source label is "env.kong". Implements [kongfig.ProviderFieldNamesSupport] to register
// the env var names (e.g. "LOG_LEVEL") for each path, so renderers can annotate values.
func Env(k *kong.Kong) kongfig.Provider {
	data := make(kongfig.ConfigData)
	names := make(map[string]string)
	for _, flag := range allFlags(k) {
		val := envValue(flag.Envs)
		path := flagPath(flag)
		if path == "" || path == "-" {
			continue
		}
		if val != "" {
			setNested(data, strings.Split(path, "."), parseNative(flag.Target, val))
		}
		if len(flag.Envs) > 0 {
			names[path] = flag.Envs[0]
		}
	}
	return &staticProvider{data: data, source: "env.kong", kind: kongfig.KindEnv, providerData: envprovider.ProviderData{}, fieldNames: names}
}

// Args returns a Provider containing only flags that were explicitly set on the CLI.
// Source label is "flags". Implements [kongfig.ProviderFieldNamesSupport] to register
// the flag names (e.g. "--log-level") for each path, so renderers can annotate values.
//
// Detection: walks ctx.Path and skips Path elements where Resolved=true
// (set by a kong.Resolver, not explicitly typed by the user on CLI).
func Args(ctx *kong.Context) kongfig.Provider {
	data := make(kongfig.ConfigData)
	names := make(map[string]string)
	for _, p := range ctx.Path {
		if p.Flag == nil {
			continue
		}
		if p.Resolved {
			continue
		}
		flag := p.Flag
		if rawConfigTag(flag) == "-" {
			continue
		}
		path := flagPath(flag)
		var val any
		if flag.Target.IsValid() {
			val = flag.Target.Interface()
		}
		setNested(data, strings.Split(path, "."), val)
		names[path] = "--" + flag.Name
	}
	return &argsProvider{data: data, fieldNames: names}
}

// AppNameOption returns a [kong.Option] that sets the application name from ctx.
// Equivalent to kong.Name(kongfig.AppName(ctx)), falling back to "" if no name
// is stored in ctx. Pairs with [kongfig.WithAppName] to propagate the app name
// from one call site through to both the file discoverer and the kong application.
//
//	ctx = kongfig.WithAppName(ctx, "myapp")
//	k := kong.Must(&cli{}, kongprovider.AppNameOption(ctx), ...)
func AppNameOption(ctx context.Context) kong.Option {
	return kong.Name(kongfig.AppName(ctx))
}

// MustLoadAll is a convenience wrapper that loads the env and flags layers from
// a parsed kong context into kf in the correct priority order (env before flags).
// Equivalent to calling kf.MustLoad(ctx, Env(k), opts...) followed by kf.MustLoad(ctx, Args(kctx), opts...).
// opts are forwarded to both Load calls (e.g. [kongfig.WithSilenceCollisions]).
// Panics on error (matches MustLoad semantics).
func MustLoadAll(ctx context.Context, k *kong.Kong, kctx *kong.Context, kf *kongfig.Kongfig, opts ...kongfig.LoadOption) {
	kf.MustLoad(ctx, Env(k), opts...)
	kf.MustLoad(ctx, Args(kctx), opts...)
}

// LoadConfigPaths loads config files referenced by flags whose kongfig tag
// includes the config-path option. Call after [MustLoadAll] (or after loading
// env and args) so that flag values are available when resolving file paths.
//
// An optional integer priority controls load order — lower numbers load first;
// unprioritized flags follow in kong model discovery order:
//
//	Config string `name:"config" short:"c" config:"-" kongfig:",config-path" optional:"" type:"path"`
//	SystemConfig string `name:"system-config" kongfig:",config-path=0" optional:"" type:"path"`
//
// The file format is determined by extension using parsers registered on kf.
// Flags with empty values are silently skipped.
// Returns on the first error.
func LoadConfigPaths(ctx context.Context, k *kong.Kong, kf *kongfig.Kongfig, opts ...kongfig.LoadOption) error {
	type entry struct {
		path        string
		priority    int
		hasPriority bool
	}
	var entries []entry
	for _, flag := range allFlags(k) {
		ft := schema.ParseFieldTag(flag.Tag.Get("kongfig"), flag.Name)
		if !ft.IsConfigPath {
			continue
		}
		if flag.Target.Kind() != reflect.String {
			continue
		}
		e := entry{path: flag.Target.String()}
		if ft.ConfigPathPriority != nil {
			e.priority = *ft.ConfigPathPriority
			e.hasPriority = true
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		pi, pj := entries[i], entries[j]
		if pi.hasPriority != pj.hasPriority {
			return pi.hasPriority
		}
		if pi.hasPriority {
			return pi.priority < pj.priority
		}
		return false
	})
	for _, e := range entries {
		if e.path == "" {
			continue
		}
		parser, err := kongfig.ParserForPath(e.path, kf.Parsers())
		if err != nil {
			return fmt.Errorf("kongprovider: config flag: %w", err)
		}
		if err := kf.Load(ctx, fileprovider.New(e.path, parser), opts...); err != nil {
			return err
		}
	}
	return nil
}

// MustLoadConfigPaths is like [LoadConfigPaths] but panics on error.
func MustLoadConfigPaths(ctx context.Context, k *kong.Kong, kf *kongfig.Kongfig, opts ...kongfig.LoadOption) {
	if err := LoadConfigPaths(ctx, k, kf, opts...); err != nil {
		panic(err)
	}
}

// argsProvider is Args' provider; also implements OutputProvider and ProviderFieldNamesSupport.
type argsProvider struct {
	data       kongfig.ConfigData
	fieldNames map[string]string
}

func (p *argsProvider) Load(_ context.Context) (kongfig.ConfigData, error) { return p.data, nil }
func (*argsProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: "flags", Kind: kongfig.KindFlags}
}
func (*argsProvider) ProviderData() kongfig.ProviderData { return flagsProviderData{} }
func (p *argsProvider) FieldNames() map[string]string    { return p.fieldNames }

// Bind returns a Renderer that writes --flag=value lines.
func (*argsProvider) Bind(s kongfig.Styler) kongfig.Renderer {
	return &flagsRenderer{s: s}
}

// flagsRenderer renders config as --flag=value lines.
type flagsRenderer struct {
	s kongfig.Styler
}

func (r *flagsRenderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
	if !render.AlignSources(ctx) {
		return renderFlags(ctx, w, r.s, data, "", false)
	}
	var buf bytes.Buffer
	if err := renderFlags(ctx, &buf, r.s, data, "", true); err != nil {
		return err
	}
	return render.AlignAnnotations(buf.String(), w)
}

// resolveFlagKey resolves the --flag key using registered field names.
// Returns ("", true) when fieldNames is set but has no binding for path/source.
func resolveFlagKey(fieldNames kongfig.PathFieldNames, path string, rv kongfig.RenderedValue, isRV bool) (key string, skip bool) {
	generated := "--" + strings.ReplaceAll(path, ".", "-")
	if fieldNames == nil || !isRV {
		return generated, false
	}
	names, bound := fieldNames[path]
	name := names[rv.Source.Layer.ID]
	if !bound || name == "" {
		return "", true
	}
	return name, false
}

// appendAnn appends a source annotation to line using the appropriate separator.
// When align is true, uses [render.AnnMarker] as separator for two-pass alignment.
func appendAnn(line, ann string, s kongfig.Styler, align bool) string {
	if align {
		return line + render.AnnMarker + "  " + s.Comment("# ") + ann
	}
	return line + "  " + s.Comment("# ") + ann
}

func renderFlags(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, prefix string, align bool) error {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	noComments := render.NoComments(ctx)
	fieldNames := render.FieldNames(ctx)

	for _, k := range keys {
		v := data[k]
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		if sub, ok := v.(kongfig.ConfigData); ok {
			if err := renderFlags(ctx, w, s, sub, path, align); err != nil {
				return err
			}
			continue
		}

		// Unwrap RenderedValue
		rv, isRV := v.(kongfig.RenderedValue)
		var leafVal any
		if isRV {
			leafVal = rv.Value
		} else {
			leafVal = v
		}

		flagKey, skip := resolveFlagKey(fieldNames, path, rv, isRV)
		if skip {
			fmt.Fprintln(w, s.Comment("# "+path+"="+fmt.Sprintf("%v", leafVal)+"  # (no flag binding)"))
			continue
		}
		line := s.Key(flagKey) + "=" + render.Value(s, v, fmt.Sprintf("%v", leafVal))
		if !noComments && isRV {
			if ann := render.Annotation(ctx, rv, path, s); ann != "" {
				line = appendAnn(line, ann, s, align)
			}
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// allFlags returns a deduplicated flat list of all flags from all nodes in the application.
func allFlags(k *kong.Kong) []*kong.Flag {
	seen := make(map[*kong.Flag]bool)
	var out []*kong.Flag
	for _, group := range k.Model.AllFlags(false) {
		for _, f := range group {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

// flagPath returns the kongfig key path for a flag.
// Reads the raw config:"" struct tag first; falls back to hyphen→dot of flag name.
func flagPath(flag *kong.Flag) string {
	if cfg := rawConfigTag(flag); cfg != "" && cfg != "-" {
		return cfg
	}
	return strings.ReplaceAll(flag.Name, "-", ".")
}

// rawConfigTag returns the value of the config:"" struct tag on the flag, if any.
func rawConfigTag(flag *kong.Flag) string {
	return flag.Tag.Get("config")
}

// envValue returns the value of the first env var in the list that is set.
func envValue(envs []string) string {
	for _, e := range envs {
		if v, ok := os.LookupEnv(e); ok {
			return v
		}
	}
	return ""
}

// setNested inserts val at the dot-path represented by parts into m.
func setNested(m kongfig.ConfigData, parts []string, val any) {
	if len(parts) == 1 {
		m[parts[0]] = val
		return
	}
	sub, ok := m[parts[0]].(kongfig.ConfigData)
	if !ok {
		sub = make(kongfig.ConfigData)
		m[parts[0]] = sub
	}
	setNested(sub, parts[1:], val)
}

// parseNative converts the string s to the native scalar type of target.
// For bool, int*, uint*, and float* kinds it parses s and returns the Go value;
// for all other kinds (string, slice, struct, …) it returns s unchanged.
func parseNative(target reflect.Value, s string) any {
	if !target.IsValid() {
		return s
	}
	switch target.Kind() {
	case reflect.Bool:
		if v, err := strconv.ParseBool(s); err == nil {
			return v
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v, err := strconv.ParseUint(s, 10, 64); err == nil {
			return v
		}
	case reflect.Float32, reflect.Float64:
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	default:
		// string, slice, struct, etc: return as-is
	}
	return s
}
