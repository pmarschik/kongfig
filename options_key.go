package kongfig

import (
	"context"
	"sync/atomic"
)

// options is the shared typed key-value bag used by all option phases
// (load, render, get). The zero value is ready to use.
type options struct{ m map[any]any }

func (o *options) bind(k, v any) {
	if o.m == nil {
		o.m = make(map[any]any)
	}
	o.m[k] = v
}

func (o options) read(k any) (any, bool) {
	if o.m == nil {
		return nil, false
	}
	v, ok := o.m[k]
	return v, ok
}

// bindInner stores val under innerKey within the sub-options at containerKey.
func (o *options) bindInner(containerKey, innerKey, val any) {
	var inner options
	if v, ok := o.read(containerKey); ok {
		if existing, ok := v.(options); ok {
			inner = existing
		}
	}
	inner.bind(innerKey, val)
	o.bind(containerKey, inner)
}

// readInner reads innerKey from the sub-options at containerKey.
func (o options) readInner(containerKey, innerKey any) (any, bool) {
	v, ok := o.read(containerKey)
	if !ok {
		return nil, false
	}
	inner, ok := v.(options)
	if !ok {
		return nil, false
	}
	return inner.read(innerKey)
}

// readOpts is a typed helper for reading from an options bag within the package.
func readOpts[T any](o options, k any) (T, bool) {
	v, ok := o.read(k)
	if !ok {
		var z T
		return z, false
	}
	t, ok := v.(T)
	return t, ok
}

// optionsKey is the shared unexported base for all typed extension keys.
// It provides a globally unique ID via an atomic counter so keys never collide,
// even across packages.
type optionsKey struct{ id uint64 }

var optionsKeyCounter atomic.Uint64

func newKey() optionsKey { return optionsKey{id: optionsKeyCounter.Add(1)} }

// LoadOptionsKey is a typed extension key for values carried through [LoadOption].
// Create one at package level with [NewLoadOptionsKey]; inject a value with
// [LoadOptionsKey.Bind] and retrieve it inside a [Provider.Load] call with [LoadOptionsKey.Read].
type LoadOptionsKey[T any] struct{ optionsKey }

// NewLoadOptionsKey returns a new unique [LoadOptionsKey][T].
// Call once per extension domain at package level.
func NewLoadOptionsKey[T any]() LoadOptionsKey[T] { return LoadOptionsKey[T]{newKey()} }

// Bind returns a LoadOption that stores val under this key.
func (k LoadOptionsKey[T]) Bind(val T) LoadOption {
	return func(c *loadOptions) { c.opts.bind(k, val) }
}

// Read returns the value stored under this key from ctx.
// Returns the zero value and false if not set.
func (k LoadOptionsKey[T]) Read(ctx context.Context) (T, bool) {
	lc := loadOptionsFromCtx(ctx)
	if lc == nil {
		var zero T
		return zero, false
	}
	return readOpts[T](lc.opts, k)
}

// RenderOptionsKey is a typed extension key for values carried through [RenderOption].
// Create one at package level with [NewRenderOptionsKey]; inject a value with
// [RenderOptionsKey.Bind] and retrieve it inside a [Renderer.Render] call with [RenderOptionsKey.Read].
type RenderOptionsKey[T any] struct{ optionsKey }

// NewRenderOptionsKey returns a new unique [RenderOptionsKey][T].
// Call once per extension domain at package level.
func NewRenderOptionsKey[T any]() RenderOptionsKey[T] { return RenderOptionsKey[T]{newKey()} }

// Bind returns a RenderOption that stores val under this key.
func (k RenderOptionsKey[T]) Bind(val T) RenderOption {
	return func(ro *renderOptions) { ro.bind(k, val) }
}

// Read returns the value stored under this key from ctx.
// Returns the zero value and false if not set.
func (k RenderOptionsKey[T]) Read(ctx context.Context) (T, bool) {
	ro := renderOptsFromCtx(ctx)
	return readOpts[T](ro, k)
}

// WithCtx returns a context with val stored under this key in the render options bag.
func (k RenderOptionsKey[T]) WithCtx(ctx context.Context, val T) context.Context {
	ro := renderOptsFromCtx(ctx)
	ro.bind(k, val)
	return withRenderOptsCtx(ctx, ro)
}

// getOptionsKey is the unexported counterpart for [GetOption] extensions.
// Used internally for path and strict; not publicly extensible.
type getOptionsKey[T any] struct{ optionsKey }

func newGetOptionsKey[T any]() getOptionsKey[T] {
	return getOptionsKey[T]{newKey()}
}

// bindGet returns a GetOption that stores val under key in the get options bag.
func bindGet[T any](key getOptionsKey[T], val T) GetOption {
	return func(c *getOptions) { c.opts.bind(key, val) }
}

// readGet retrieves val stored under key in the get options bag.
func readGet[T any](c *getOptions, key getOptionsKey[T]) (T, bool) {
	return readOpts[T](c.opts, key)
}

// PathMetaKey is a typed key for per-path metadata stored in the render context.
// Create one at package level with [NewPathMetaKey]; retrieve values with
// [PathMetaKey.Get] or [PathMetaKey.GetAll]; inject for testing with [PathMetaKey.WithCtx].
type PathMetaKey[T any] struct{ optionsKey }

// NewPathMetaKey returns a new unique [PathMetaKey][T].
// Call once per metadata domain at package level.
func NewPathMetaKey[T any]() PathMetaKey[T] { return PathMetaKey[T]{newKey()} }

// Get returns the metadata value stored for path under this key in ctx.
func (k PathMetaKey[T]) Get(ctx context.Context, path string) (T, bool) {
	m := k.GetAll(ctx)
	if m == nil {
		var zero T
		return zero, false
	}
	v, ok := m[path]
	return v, ok
}

// GetAll returns the full path → value map stored under this key in ctx.
func (k PathMetaKey[T]) GetAll(ctx context.Context) map[string]T {
	ro := renderOptsFromCtx(ctx)
	v, ok := ro.readInner(pathMetaContainerKey{}, k)
	if !ok {
		return nil
	}
	m, ok := v.(map[string]T)
	if !ok {
		return nil
	}
	return m
}

// WithCtx returns a context with entries stored under this key.
func (k PathMetaKey[T]) WithCtx(ctx context.Context, entries map[string]T) context.Context {
	ro := renderOptsFromCtx(ctx)
	ro.bindInner(pathMetaContainerKey{}, k, entries)
	return withRenderOptsCtx(ctx, ro)
}

// OptionsKey[T] is a typed extension key for cross-cutting context values
// not tied to a specific phase (load, render, or get).
// Create keys with [NewOptionsKey]; store/retrieve with [OptionsKey.With] and [OptionsKey.From].
type OptionsKey[T any] struct{ optionsKey }

// NewOptionsKey returns a new unique OptionsKey[T].
func NewOptionsKey[T any]() OptionsKey[T] { return OptionsKey[T]{newKey()} }

type contextOptsKey struct{}

func contextOptsFromCtx(ctx context.Context) options {
	if o, ok := ctx.Value(contextOptsKey{}).(options); ok {
		return o
	}
	return options{}
}

func withContextOptsCtx(ctx context.Context, o options) context.Context {
	return context.WithValue(ctx, contextOptsKey{}, o)
}

// With returns a new context with val stored under this key.
func (k OptionsKey[T]) With(ctx context.Context, val T) context.Context {
	o := contextOptsFromCtx(ctx)
	o.bind(k, val)
	return withContextOptsCtx(ctx, o)
}

// From returns the value stored under this key in ctx.
// Returns the zero value and false if not set.
func (k OptionsKey[T]) From(ctx context.Context) (T, bool) {
	o := contextOptsFromCtx(ctx)
	return readOpts[T](o, k)
}
