// Package show provides reusable kong CLI flags for the kongfig config-show command.
//
// Embed [Flags] for full output control: --format, --layers.
// Embed [SimpleFlags] for minimal control: --plain, --layers (no format selection).
package show

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	envprovider "github.com/pmarschik/kongfig/providers/env"
	render "github.com/pmarschik/kongfig/render"
	"github.com/pmarschik/kongfig/style/plain"
)

// FormatFlag adds --format.
type FormatFlag struct {
	Format string `name:"format" config:"-" default:"" enum:"${kongrender_formats=,yaml,env,flags}" help:"Output format (${enum})."`
}

// OutputPlainFlag adds --plain / --no-plain.
type OutputPlainFlag struct {
	Plain bool `name:"plain" config:"-" negatable:"" help:"Plain output without colors."`
}

// LayersFlag adds --layers and --verbose.
// --verbose controls how many source label segments are shown in layer headers:
//   - 0 (default): group env.* providers into a single "env" layer
//   - 1+: show each provider with its full source label (env.tag, env.kong, etc.)
type LayersFlag struct {
	Layers  bool `name:"layers"  config:"-" help:"Render each config layer separately."`
	Verbose int  `name:"verbose" short:"v"  config:"-"                                  type:"counter" help:"Show detailed source labels in --layers output. Repeat for more detail."`
}

// RedactedFlag adds --redacted / --no-redacted.
// When --no-redacted (default), values at paths registered via kongfig.WithRedacted
// are replaced with "***" in output. Pass --redacted to reveal them.
type RedactedFlag struct {
	ShowRedacted bool `name:"redacted" config:"-" negatable:"" help:"Show redacted values (default: hidden)."`
}

// SourcesFlag adds per-source visibility toggles as negatable boolean flags.
// Defaults are controlled via kong.Vars at kong.New() time so each app can
// choose which sources are visible without --no-X flags:
//
//	kong.New(&cli, kong.Vars{
//	    "kongshow_defaults": "false",  // hide defaults layer by default
//	    "kongshow_env":      "true",
//	    "kongshow_file":     "true",
//	    "kongshow_flags":    "true",
//	})
//
// When a Var is not injected it falls back to "true" (source shown by default).
// Embedded in [SimpleFlags].
type SourcesFlag struct {
	Defaults bool `name:"defaults" config:"-" default:"${kongshow_defaults=true}" negatable:"" help:"Include defaults layer."`
	Env      bool `name:"env"      config:"-" default:"${kongshow_env=true}"      negatable:"" help:"Include env layers (env.tag, env.kong, …)."`
	File     bool `name:"file"     config:"-" default:"${kongshow_file=true}"     negatable:"" help:"Include file layers (xdg, workdir, explicit)."`
	Flags    bool `name:"flags"    config:"-" default:"${kongshow_flags=true}"    negatable:"" help:"Include flags layer."`
}

// FilterSource converts the boolean toggles to a [kongfig.MatchesFilterSource] filter slice.
func (f SourcesFlag) FilterSource() []string {
	return render.BuildFilterSource(map[string]bool{
		"defaults": f.Defaults,
		"env":      f.Env,
		"file":     f.File,
		"flags":    f.Flags,
	})
}

// SourceListFlag adds a --sources flag with two syntactic modes determined by
// the entries the user provides. Empty list shows all sources.
//
// Overlay mode (hide specific sources from show-all):
//
//	--sources=-defaults          show all except defaults
//	--sources=-defaults,-flags   show all except defaults and flags
//
// Allowlist mode (show only specific sources):
//
//	--sources=env,file           show only env and file
//	--sources=+env,+file         same, using explicit + prefix to signal intent
//	--sources=+env,-workdir      allowlist env; additionally exclude workdir
//
// Prefixes: + or no prefix = include (allowlist entry); - = exclude (overlay/denylist entry).
// [kongfig.MatchesFilterSource] handles the combined semantics: only-no-* entries act as a
// denylist; positive entries form an allowlist and no-* additionally excludes within it.
// Embedded in [Flags].
type SourceListFlag struct {
	Sources []string `name:"sources" config:"-" help:"Filter sources: plain or + to include (e.g. env,file or +env,+file), - to hide from all (e.g. -defaults). Empty shows all." sep:","`
}

// FilterSource translates the syntax into a [kongfig.MatchesFilterSource] filter slice:
// -foo → no-foo (denylist); +foo → foo (explicit allowlist); plain foo → foo (allowlist).
func (f SourceListFlag) FilterSource() []string {
	if len(f.Sources) == 0 {
		return nil
	}
	out := make([]string, 0, len(f.Sources))
	for _, s := range f.Sources {
		switch {
		case strings.HasPrefix(s, "-"):
			out = append(out, "no-"+s[1:])
		case strings.HasPrefix(s, "+"):
			out = append(out, s[1:]) // strip + marker, keep as allowlist entry
		default:
			out = append(out, s)
		}
	}
	return out
}

// Flags embeds --format, --layers, --redacted, and --sources flags. It does not
// include --plain; the caller controls styling by passing the appropriate [kongfig.Styler]
// to Render.
//
// For apps that only need a plain/colored toggle without format selection,
// use [SimpleFlags] instead.
//
// Set DefaultRenderer to override the fallback renderer used when a layer
// has no native parser and --format is not specified (e.g. a derived layer).
// Defaults to YAML when nil.
type Flags struct {
	DefaultRenderer kongfig.OutputProvider `kong:"-"`
	FormatFlag      `embed:""`
	SourceListFlag  `embed:""`
	LayersFlag      `embed:""`
	RedactedFlag    `embed:""`
}

// Options returns the [kongfig.RenderOption] slice derived from flag state.
// Pass the result (along with caller-specific options) to Render,
// [kongfig.RenderLayers], or [kongfig.RenderWith].
func (f *Flags) Options(k *kongfig.Kongfig) []kongfig.RenderOption {
	var opts []kongfig.RenderOption
	if f.ShowRedacted {
		opts = append(opts, kongfig.WithRenderShowRedacted())
	}
	if filters := f.FilterSource(); len(filters) > 0 {
		opts = append(opts, kongfig.WithRenderFilterSource(filters))
	}
	if f.Verbose > 0 {
		opts = append(opts, kongfig.WithRenderVerboseSources(verboseSources(k.Layers())))
	}
	return opts
}

// Render writes configuration output to w using k as the data source.
// Pass plain.New() as s to disable colors; pass nil to use plain automatically.
func (f *Flags) Render(ctx context.Context, w io.Writer, k *kongfig.Kongfig, s kongfig.Styler, opts ...kongfig.RenderOption) error {
	if s == nil {
		s = plain.New()
	}
	// Merge flag-derived options first, then caller opts (caller wins).
	allOpts := append(f.Options(k), opts...)

	renderers := buildFormatRenderers(k)
	format := effectiveFormat(f.Format, k)
	if f.Layers {
		if format != "" {
			allOpts = append(allOpts, kongfig.WithRenderFormat(format))
		}
		return renderPerLayer(ctx, w, k, format, f.Verbose, s, f.DefaultRenderer, renderers, allOpts...)
	}
	return k.RenderWith(ctx, w, &dataRenderer{format: format, s: s, renderers: renderers}, allOpts...)
}

// SimpleFlags embeds --plain, --layers, --redacted, and per-source negatable
// boolean flags (--no-defaults, --no-env, --no-file, --no-flags). It does not
// include --format; use this for apps that render in a fixed style without
// format selection.
//
// Set DefaultRenderer to override the fallback renderer used when a layer
// has no native parser. Defaults to YAML when nil.
type SimpleFlags struct {
	DefaultRenderer kongfig.OutputProvider `kong:"-"`
	LayersFlag      `embed:""`
	OutputPlainFlag `embed:""`
	RedactedFlag    `embed:""`
	SourcesFlag     `embed:""`
}

// Options returns the [kongfig.RenderOption] slice derived from flag state.
func (f *SimpleFlags) Options(k *kongfig.Kongfig) []kongfig.RenderOption {
	var opts []kongfig.RenderOption
	if f.ShowRedacted {
		opts = append(opts, kongfig.WithRenderShowRedacted())
	}
	if filters := f.FilterSource(); len(filters) > 0 {
		opts = append(opts, kongfig.WithRenderFilterSource(filters))
	}
	if f.Verbose > 0 {
		opts = append(opts, kongfig.WithRenderVerboseSources(verboseSources(k.Layers())))
	}
	return opts
}

// Render writes configuration output to w using k as the data source.
// When Plain is true the styler is overridden with plain.New().
func (f *SimpleFlags) Render(ctx context.Context, w io.Writer, k *kongfig.Kongfig, s kongfig.Styler, opts ...kongfig.RenderOption) error {
	if s == nil || f.Plain {
		s = plain.New()
	}
	allOpts := append(f.Options(k), opts...)

	renderers := buildFormatRenderers(k)
	format := effectiveFormat("", k)
	if f.Layers {
		return renderPerLayer(ctx, w, k, format, f.Verbose, s, f.DefaultRenderer, renderers, allOpts...)
	}
	return k.RenderWith(ctx, w, &dataRenderer{format: format, s: s, renderers: renderers}, allOpts...)
}

func renderData(ctx context.Context, w io.Writer, format string, data kongfig.ConfigData, s kongfig.Styler, renderers map[string]kongfig.OutputProvider) error {
	switch format {
	case "env":
		r := envprovider.Provider("", ".").Bind(s)
		return r.Render(ctx, w, data)
	case "flags":
		return renderFlagsFormat(ctx, w, s, data)
	default:
		if format != "" {
			if op, ok := renderers[format]; ok {
				return op.Bind(s).Render(ctx, w, data)
			}
		}
		// yaml (default) or unknown format: use yaml parser
		r := yamlparser.Default.Bind(s)
		return r.Render(ctx, w, data)
	}
}

// renderFlagsFormat renders config as --flag=value lines.
// This mirrors kong/provider's flagsRenderer without depending on its unexported type.
func renderFlagsFormat(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData) error {
	return renderFlagsMap(ctx, w, s, data, "")
}

// resolveFlagKey resolves the --flag key for a given path using registered field names.
// Returns the flag key and whether to skip this entry (unbound when field names are set).
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

func renderFlagsMap(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, prefix string) error {
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
			if err := renderFlagsMap(ctx, w, s, sub, path); err != nil {
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
				line += "  " + s.Comment("# ") + ann
			}
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// buildFormatRenderers extracts a format-name → OutputProvider map from all parsers
// registered with k (via RegisterParsers or auto-registered on load).
func buildFormatRenderers(k *kongfig.Kongfig) map[string]kongfig.OutputProvider {
	out := make(map[string]kongfig.OutputProvider)
	for _, p := range k.Parsers() {
		namer, ok := p.(kongfig.ParserNamer)
		if !ok {
			continue
		}
		op, ok := p.(kongfig.OutputProvider)
		if !ok {
			continue
		}
		if _, exists := out[namer.Format()]; !exists {
			out[namer.Format()] = op
		}
	}
	return out
}

// effectiveFormat returns the format to use for rendering.
// If explicit is non-empty it is returned as-is (user override wins).
// Otherwise the first registered parser that implements both ParserNamer and
// OutputProvider is used, falling back to "" (which renderData maps to YAML).
func effectiveFormat(explicit string, k *kongfig.Kongfig) string {
	if explicit != "" {
		return explicit
	}
	for _, p := range k.Parsers() {
		namer, ok := p.(kongfig.ParserNamer)
		if !ok {
			continue
		}
		if _, ok := p.(kongfig.OutputProvider); !ok {
			continue
		}
		if f := namer.Format(); f != "" {
			return f
		}
	}
	return ""
}

// dataRenderer adapts renderData to the [kongfig.Renderer] interface.
// Used by [Flags.Render] and [SimpleFlags.Render] for the non-layers path.
type dataRenderer struct {
	s         kongfig.Styler
	renderers map[string]kongfig.OutputProvider
	format    string
}

func (r *dataRenderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
	return renderData(ctx, w, r.format, data, r.s, r.renderers)
}

func renderPerLayer(ctx context.Context, w io.Writer, k *kongfig.Kongfig, format string, verbose int, s kongfig.Styler, defaultRenderer kongfig.OutputProvider, renderers map[string]kongfig.OutputProvider, opts ...kongfig.RenderOption) error {
	layerOpts := opts
	if verbose == 0 {
		layerOpts = append(layerOpts, kongfig.WithRenderGroupEnvLayers())
	}
	return k.RenderLayers(ctx, func(lctx context.Context, layer kongfig.Layer, data kongfig.ConfigData) error {
		if len(data) == 0 {
			if verbose > 0 {
				fmt.Fprintf(w, "%s %s %s\n\n", s.Comment("# ==="), layerHeader(lctx, layer, s), s.Comment("=== (empty)"))
			}
			return nil
		}
		fmt.Fprintf(w, "%s %s %s\n", s.Comment("# ==="), layerHeader(lctx, layer, s), s.Comment("==="))
		if err := renderLayerData(lctx, w, layer, format, defaultRenderer, s, renderers, data); err != nil {
			return err
		}
		fmt.Fprintln(w)
		return nil
	}, layerOpts...)
}

// renderLayerData renders a single layer using pre-prepared data. Priority order:
//  1. layer.Parser (native format — always used when available)
//  2. well-known native format for env/flags layers
//  3. format flag (fallback for layers with no native renderer, e.g. defaults)
//  4. defaultRenderer (user-configured fallback)
//  5. YAML (built-in last resort)
func renderLayerData(ctx context.Context, w io.Writer, layer kongfig.Layer, format string, defaultRenderer kongfig.OutputProvider, s kongfig.Styler, renderers map[string]kongfig.OutputProvider, data kongfig.ConfigData) error {
	// Native renderer wins — a YAML file stays YAML, a TOML file stays TOML.
	if layer.Parser != nil {
		if op, ok := layer.Parser.(kongfig.OutputProvider); ok {
			return op.Bind(s).Render(ctx, w, data)
		}
	}
	// Env and flags layers have well-known native formats.
	if layer.Meta.Kind == kongfig.KindFlags {
		return renderFlagsFormat(ctx, w, s, data)
	}
	if layer.Meta.Kind == kongfig.KindEnv {
		r := envprovider.Provider("", ".").Bind(s)
		return r.Render(ctx, w, data)
	}
	// No native renderer (e.g. defaults, derived): --format is the fallback.
	if format != "" {
		if op, ok := renderers[format]; ok {
			return op.Bind(s).Render(ctx, w, data)
		}
	}
	if defaultRenderer != nil {
		return defaultRenderer.Bind(s).Render(ctx, w, data)
	}
	r := yamlparser.Default.Bind(s)
	return r.Render(ctx, w, data)
}

// --- FlagsVars ---

// FlagsVarOpt configures FlagsVars.
type FlagsVarOpt func(*flagsVarConfig)

type flagsVarConfig struct {
	formats []string
}

// WithFormats sets explicit format enum values.
func WithFormats(formats ...string) FlagsVarOpt {
	return func(c *flagsVarConfig) { c.formats = formats }
}

// WithLoaderFormats builds the format enum from a list of registered format names.
// Always prepends "" (yaml default) and appends "env", "flags" if not present.
func WithLoaderFormats(names []string) FlagsVarOpt {
	return func(c *flagsVarConfig) {
		formats := make([]string, 0, len(names)+3)
		formats = append(formats, "")
		formats = append(formats, names...)
		for _, extra := range []string{"env", "flags"} {
			if !slices.Contains(formats, extra) {
				formats = append(formats, extra)
			}
		}
		c.formats = formats
	}
}

// FlagsVarsFromKongfig derives the --format enum from parsers registered with k.
// Call after [kongfig.Kongfig.RegisterParsers] so all supported formats are included.
// Equivalent to FlagsVars(WithLoaderFormats(formatNames(k.Parsers()))).
func FlagsVarsFromKongfig(k *kongfig.Kongfig) kong.Vars {
	names := make([]string, 0)
	for _, p := range k.Parsers() {
		if namer, ok := p.(kongfig.ParserNamer); ok {
			name := namer.Format()
			if name != "" && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	return FlagsVars(WithLoaderFormats(names))
}

// layerHeader returns the styled header string for a layer in --layers output.
//
// Priority:
//  1. If meta renders more than just the kind (e.g. a file path), use that.
//  2. If meta renders to just the kind but name carries a sub-source suffix
//     (e.g. "env.tag"), render as "kind (name)" using SourceKind + SourceData styling.
//  3. Otherwise return the kind-styled token.
func layerHeader(ctx context.Context, layer kongfig.Layer, s kongfig.Styler) string {
	rendered := layer.Meta.RenderAnnotation(ctx, s, "")
	if rendered != s.SourceKind(layer.Meta.Kind) {
		return rendered // meta adds extra detail (e.g. file path) — use it as-is
	}
	if layer.Meta.Name != layer.Meta.Kind {
		// kind renders to just "env" but name is "env.tag" — show "env (env.tag)"
		return s.SourceKind(layer.Meta.Kind) + " (" + s.SourceData(layer.Meta.Name) + ")"
	}
	return rendered
}

// verboseSources builds a map from config dot-path to the ordered list of all
// source labels (from all layers) that provided a value for that path.
// Used to populate VerboseSources for multi-source annotations.
func verboseSources(layers []kongfig.Layer) map[string][]string {
	out := make(map[string][]string)
	for _, layer := range layers {
		collectSources(layer.Data, layer.Meta.Name, "", out)
	}
	return out
}

func collectSources(data kongfig.ConfigData, source, prefix string, out map[string][]string) {
	for k, v := range data {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(kongfig.ConfigData); ok {
			collectSources(sub, source, path, out)
		} else {
			out[path] = append(out[path], source)
		}
	}
}

// FlagsVars returns kong.Vars for injecting format enum values.
func FlagsVars(opts ...FlagsVarOpt) kong.Vars {
	cfg := &flagsVarConfig{
		formats: []string{"", "yaml", "env", "flags"},
	}
	for _, o := range opts {
		o(cfg)
	}
	return kong.Vars{
		"kongrender_formats": strings.Join(cfg.formats, ","),
	}
}
