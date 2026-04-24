package kongfig

import (
	"fmt"
	"maps"
	"reflect"
)

// Codec[T] is a bidirectional codec between a config value and Go type T.
// Use [WithCodec] or [Register] + [WithCodecRegistry] to register on a [Kongfig] instance.
//
// Decode and Encode are typed: Decode returns T and Encode takes T, so there
// is no type drift between the two directions.
//
//   - Decode converts any config value to T. The input may be a string (from env
//     vars or flags) or an already-typed value (from file parsers that produce native
//     types such as a TOML datetime). Implementations should handle both cases and
//     pass through the value unchanged when it is already of type T.
//
//   - Encode converts T back to its canonical string for rendering. If nil, the
//     default %v formatting is used.
type Codec[T any] struct {
	Decode func(any) (T, error)
	Encode func(T) string // nil → fmt.Sprintf("%v", v)
}

// CodecEntry is the type-erased, registry-ready form of a [Codec][T].
// Create one with the generic [Of] function; pass it to [(*CodecRegistry).Register]:
//
//	r.Register("ip", kongfig.Of(codecs.IP))
type CodecEntry struct{ inner anyCodec }

// Of wraps c as a [CodecEntry], capturing the Go type T.
// Use with [(*CodecRegistry).Register] for method-style registration:
//
//	r.Register("ip", kongfig.Of(codecs.IP))
//	r.Register("duration", kongfig.Of(codecs.Duration))
func Of[T any](c Codec[T]) CodecEntry {
	return CodecEntry{inner: makeAnyCodec[T](c)}
}

// DecodeOnly creates a decode-only [CodecEntry] for per-path value normalization.
// Use with [Kongfig.RegisterCodec] when no render-time encoding is needed:
//
//	kf.RegisterCodec("tags", kongfig.DecodeOnly(splitComma))
func DecodeOnly(fn func(any) any) CodecEntry {
	return CodecEntry{inner: anyCodec{decode: func(v any) (any, error) { return fn(v), nil }}}
}

// CodecRegistry is a named + type-indexed collection of [Codec] values.
// Create one with [NewCodecRegistry]; populate it with [Register] or [(*CodecRegistry).Register].
// Install on a [Kongfig] with [WithCodecRegistry] or [WithCodec].
//
// The first registration for a given Go type T wins for auto-detection:
// all non-primitive fields in [NewFor] use that codec unless overridden by
// an explicit codec= struct tag annotation.
type CodecRegistry struct {
	byName map[string]anyCodec
	byType map[reflect.Type]anyCodec
}

// NewCodecRegistry returns an empty [CodecRegistry].
func NewCodecRegistry() *CodecRegistry {
	return &CodecRegistry{
		byName: make(map[string]anyCodec),
		byType: make(map[reflect.Type]anyCodec),
	}
}

// add stores ac in r under name. First registration per Go type wins for auto-detection.
func (r *CodecRegistry) add(name string, ac anyCodec) {
	r.byName[name] = ac
	if _, exists := r.byType[ac.goType]; !exists {
		r.byType[ac.goType] = ac // first registered per type wins
	}
}

// Register adds e to r under name and (first-wins) under its Go type.
// Returns r so calls can be chained:
//
//	r.Register("ip", kongfig.Of(codecs.IP)).
//	  Register("duration", kongfig.Of(codecs.Duration))
func (r *CodecRegistry) Register(name string, e CodecEntry) *CodecRegistry {
	r.add(name, e.inner)
	return r
}

// WithCodec registers a named [Codec][T] directly on the [Kongfig] instance.
//
//   - name is the registry key used by codec= struct tag annotations.
//   - The first registration for a given Go type T is used for auto-detection:
//     all non-primitive fields in [NewFor] whose type matches use this codec unless
//     overridden by an explicit codec= annotation.
//
// Note: WithCodec only populates the named registry. Per-path decoding is wired
// automatically by [NewFor] (which calls withCodecPathResolution after all options).
// When using [New] instead of [NewFor], call [Kongfig.RegisterCodec] to attach a
// codec directly to a specific path.
func WithCodec[T any](name string, c Codec[T]) Option {
	return func(k *Kongfig) {
		k.mu.Lock()
		defer k.mu.Unlock()
		k.cfg.codecs.add(name, makeAnyCodec[T](c))
	}
}

// WithCodecRegistry merges all codecs from r into the [Kongfig] instance's registry.
// Name entries from r overwrite existing entries; type entries follow first-wins order
// (already-registered types in the instance are not replaced).
// r is not mutated.
func WithCodecRegistry(r *CodecRegistry) Option {
	return func(k *Kongfig) {
		if r == nil {
			return
		}
		k.mu.Lock()
		defer k.mu.Unlock()
		maps.Copy(k.cfg.codecs.byName, r.byName)
		for t, ac := range r.byType {
			if _, exists := k.cfg.codecs.byType[t]; !exists {
				k.cfg.codecs.byType[t] = ac // first-wins per type
			}
		}
	}
}

// makeAnyCodec converts a typed Codec[T] to the internal type-erased anyCodec.
// If c.Encode is nil, the returned anyCodec has a nil encode — meaning no render-time
// encoding is applied and the decoded value is passed to renderers as-is.
func makeAnyCodec[T any](c Codec[T]) anyCodec {
	goType := reflect.TypeFor[T]()
	decAny := func(v any) (any, error) { return c.Decode(v) }
	var encAny func(any) string
	if c.Encode != nil {
		encAny = func(v any) string {
			if tv, ok := v.(T); ok {
				return c.Encode(tv)
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return anyCodec{decode: decAny, encode: encAny, goType: goType}
}

// anyCodec is the internal type-erased representation stored in the registry.
type anyCodec struct {
	decode func(any) (any, error)
	encode func(any) string
	goType reflect.Type
}
