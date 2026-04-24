package kongfig

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// SourceID is an opaque, auto-incremented identifier for a single Load event.
// IDs are unique within a process; ordering reflects load sequence (lower = loaded earlier).
// SourceIDs are ephemeral: values are assigned from a per-process monotonic counter and
// are NOT stable across process restarts.
type SourceID uint64

func (id SourceID) String() string { return fmt.Sprintf("%016x", uint64(id)) }

// globalSourceCounter is the process-wide counter used to stamp SourceIDs.
var globalSourceCounter atomic.Uint64

func nextSourceID() SourceID { return SourceID(globalSourceCounter.Add(1)) }

// RenderedValue wraps a leaf config value with source and redaction metadata.
// The render methods place this into the data map for every leaf; renderers unwrap it.
type RenderedValue struct {
	Value           any
	RedactedDisplay string
	Source          SourceMeta
	Redacted        bool
	// Encoded reports that Value was produced by a codec's Encode function and
	// should be styled with [Styler.Codec].
	Encoded bool
}

// ProviderData is implemented by provider-owned structs to render
// source annotation strings (e.g. a file path or env var name).
// path is the config dot-path being annotated; empty for layer headers.
// Implementations must return a single line (no newlines).
type ProviderData interface {
	RenderAnnotation(ctx context.Context, s Styler, path string) string
}

// ProviderDataSupport is an optional [Provider] extension.
// Implement it to attach rich annotation data (file path, env var name, etc.)
// to the layer this provider loads. Kongfig calls ProviderData() once at load
// time and stores the result in [LayerMeta.Data].
type ProviderDataSupport interface {
	ProviderData() ProviderData
}

// ProviderFieldNamesSupport is an optional [Provider] extension.
// Implement it to register the mapping from config dot-path to the provider-specific
// field name (e.g. "APP_DB_HOST" for env providers, "--db-host" for flag providers).
// Kongfig calls FieldNames() once after Load() and stores the result in [PathFieldNames]
// keyed by the layer's [SourceID], so renderers can look up the name per source.
//
// Field names starting with "--" are treated as flag names; all others as env var names.
// Returning nil or an empty map is valid (no field names registered for this load).
type ProviderFieldNamesSupport interface {
	FieldNames() map[string]string
}

// PathFieldNames maps dot-path → SourceID → field name (env var or flag name).
// Built automatically from [ProviderFieldNamesSupport] at load time; returned by
// [Kongfig.FieldNames] and injected into the render context by [Kongfig.RenderWith].
type PathFieldNames map[string]map[SourceID]string

// WithSourceIDCtx injects a SourceID into ctx. Called automatically by
// [LayerMeta.RenderAnnotation] before delegating to [ProviderData.RenderAnnotation],
// so ProviderData implementations can look up field names keyed by SourceID.
// Use [SourceIDFromCtx] to retrieve it.
func WithSourceIDCtx(ctx context.Context, id SourceID) context.Context {
	return context.WithValue(ctx, sourceIDKey{}, id)
}

// SourceIDFromCtx returns the SourceID injected by [LayerMeta.RenderAnnotation].
// Returns 0 if not set. Use in [ProviderData.RenderAnnotation] implementations
// to look up field names from [RenderFieldNames].
func SourceIDFromCtx(ctx context.Context) SourceID {
	if id, ok := ctx.Value(sourceIDKey{}).(SourceID); ok {
		return id
	}
	return 0
}

type sourceIDKey struct{}

// LayerMeta is the concrete metadata descriptor for one loaded layer.
// It is stamped by Kongfig at load time and stored on [Layer.Meta],
// in [Provenance], and returned by [Kongfig.SourceFor].
type LayerMeta struct {
	Data      ProviderData // nil for plain sources with no rich metadata
	Timestamp time.Time    // wall clock time when this layer was loaded
	Name      string       // source label, e.g. "env.tag", "file"
	Kind      string       // provider category, e.g. [KindEnv], [KindFile]
	Format    string       // parser format name: "yaml", "toml", "json"; empty for non-parsers
	ID        SourceID     // unique per Load call; lower value = loaded earlier
}

// RenderAnnotation renders the full annotation string using the provider data.
// Delegates to Data.RenderAnnotation when Data is non-nil; otherwise renders
// just the kind. Panics if Data returns a multi-line string.
// For env kinds, reads verboseSources from ctx via renderOptsFromCtx to produce
// the verbose label like "[env.tag, env.kong]".
func (m LayerMeta) RenderAnnotation(ctx context.Context, s Styler, path string) string {
	// Inject source ID so ProviderData.RenderAnnotation can look up field names.
	ctx = WithSourceIDCtx(ctx, m.ID)

	kind := m.Kind
	if m.Kind == KindEnv || strings.HasPrefix(m.Kind, KindEnv+".") {
		ro := renderOptsFromCtx(ctx)
		kind = envSourceLabel(path, ro)
	}
	if m.Data != nil {
		data := m.Data.RenderAnnotation(ctx, s, path)
		if strings.ContainsRune(data, '\n') {
			panic(fmt.Sprintf("kongfig: ProviderData.RenderAnnotation must return a single line; %T returned %q", m.Data, data))
		}
		if data != "" {
			return s.SourceKind(kind) + " (" + data + ")"
		}
	}
	return s.SourceKind(kind)
}

// SourceMeta is the per-path source attribution stored in [Provenance].
// It records which layer last wrote a config path.
// Future fields (e.g. parser position) will be added here.
type SourceMeta struct {
	Layer LayerMeta
}

// inferKind derives a Kind from a source name using well-known prefix conventions.
// "env.tag" → KindEnv, "flags.kong" → KindFlags.
// Falls back to the full name when no prefix matches.
// File providers always set Kind explicitly, so "file." is not inferred here.
func inferKind(name string) string {
	for _, kind := range []string{KindEnv, KindFile, KindFlags, KindDefaults, KindDerived} {
		if name == kind || strings.HasPrefix(name, kind+".") {
			return kind
		}
	}
	return name
}

// Well-known source kind constants. Used as [LayerMeta.Kind] by built-in providers.
// Custom providers may use any string; these are provided to avoid typos.
const (
	KindEnv      = "env"
	KindFile     = "file"
	KindFlags    = "flags"
	KindDefaults = "defaults"
	KindDerived  = "derived"
)
