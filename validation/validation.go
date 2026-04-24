package validation

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/schema"
	vexpr "github.com/pmarschik/kongfig/validation/expr"
)

// Severity indicates the seriousness of a validation violation.
type Severity uint8

const (
	SeverityError   Severity = iota // zero value; Err() returns non-nil
	SeverityWarning                 // should fix; collected but non-fatal
	SeverityInfo                    // purely informational
	SeverityHint                    // optional improvement suggestion
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	case SeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

// Event is passed to per-key validators.
type Event struct {
	Value any
	Key   string
}

// FieldViolation is a single diagnostic produced by a field or annotation validator.
// Path is not set by the validator — the framework injects the registered config path
// automatically before adding the violation to [Diagnostics].
type FieldViolation struct {
	Message  string
	Code     string
	Severity Severity
}

// PathSource pairs a config path with the layer that last wrote it.
// Source is nil when provenance is unavailable for this path.
type PathSource struct {
	Source *kongfig.SourceMeta
	Path   string
}

// Violation is the unified output type in Diagnostics.
type Violation struct {
	Message  string
	Code     string
	Paths    []PathSource
	Severity Severity
}

// LayerViolation holds per-load violations for a specific layer.
type LayerViolation struct {
	Layer      kongfig.Layer
	Violations []Violation
}

// Diagnostics is the bag of violations returned by Validate().
// It is not an error type; call Err() to obtain an error when SeverityError violations exist.
type Diagnostics struct {
	Violations     []Violation      // final-state violations from Validate()
	LoadViolations []LayerViolation // per-load violations accumulated by WithNotifyOnLoad / WithValidateOnLoad hooks
}

// Err returns a non-nil error summarizing all SeverityError violations in Violations
// if any exist; nil otherwise. LoadViolations are intentionally excluded: they
// represent per-load rejections (already handled at load time) and should not
// block on the final merged config check. Nil-safe: (*Diagnostics)(nil).Err() returns nil.
// Callers retain the *Diagnostics for structured access to individual violations.
func (d *Diagnostics) Err() error {
	if d == nil {
		return nil
	}
	var msgs []string
	for _, v := range d.Violations {
		if v.Severity == SeverityError {
			msgs = append(msgs, formatViolation(v, ""))
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return fmt.Errorf("validation: %s", strings.Join(msgs, "; "))
}

func formatViolation(v Violation, layer string) string {
	strs := make([]string, len(v.Paths))
	for i, ps := range v.Paths {
		strs[i] = ps.Path
	}
	paths := strings.Join(strs, ", ")
	if layer != "" {
		return fmt.Sprintf("%s (layer %s): %s", paths, layer, v.Message)
	}
	if paths != "" {
		return fmt.Sprintf("%s: %s", paths, v.Message)
	}
	return v.Message
}

// ── Registry ──────────────────────────────────────────────────────────────────

// Registry holds annotation handlers keyed by tag name.
//
// Choose the right constructor for your use case:
//
//   - [DefaultRegistry] — the package-level shared registry; [New] holds a live
//     reference to it, so annotations registered after New() are visible.
//   - [NewRegistryFromDefaults] — a new registry seeded from the current defaults
//     (snapshot); changes to DefaultRegistry after this call are not reflected.
//   - [NewEmptyRegistry] — a blank registry with no handlers, not even "required".
//
// Pass a registry to a Validator with [WithRegistry].
type Registry struct {
	handlers map[string]annotationHandler
	mu       sync.RWMutex
}

// defaultRegistry is the package-level registry.
// Populated at init time with the built-in annotation handlers; see builtins.go.
var defaultRegistry = &Registry{
	handlers: make(map[string]annotationHandler),
}

// DefaultRegistry returns the package-level annotation registry.
// It is pre-seeded with built-in handlers: required, min, max, len, oneof,
// pattern, email, url, hostname.
// Validators created with [NewWithDefaults] hold a live reference to this registry.
func DefaultRegistry() *Registry { return defaultRegistry }

// NewRegistryFromDefaults creates a Registry pre-seeded with the handlers
// currently in [DefaultRegistry]. It is an independent copy — subsequent changes
// to DefaultRegistry do not affect it, and vice versa.
func NewRegistryFromDefaults() *Registry {
	return defaultRegistry.clone()
}

// clone returns an independent copy of r under a read lock.
func (r *Registry) clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := &Registry{handlers: make(map[string]annotationHandler, len(r.handlers))}
	maps.Copy(c.handlers, r.handlers)
	return c
}

// NewEmptyRegistry creates a Registry with no annotation handlers.
// Even the built-in "required" annotation is absent; add only what you need.
func NewEmptyRegistry() *Registry {
	return &Registry{handlers: make(map[string]annotationHandler)}
}

// Register adds or replaces the handler for the given annotation tag name.
func (r *Registry) Register(name string, fn AnnotationFieldFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = fn
}

func (r *Registry) lookup(name string) (annotationHandler, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}

// RegisterAnnotation registers fn on [DefaultRegistry].
// All Validators created with [NewWithDefaults] (which hold a live reference) will see
// the annotation on their next [Validator.Validate] call.
func RegisterAnnotation(name string, fn AnnotationFieldFunc) {
	defaultRegistry.Register(name, fn)
}

// ── Annotation handlers ───────────────────────────────────────────────────────

// annotationHandler is the internal interface for annotation validators.
// All registered handlers implement this via [AnnotationFieldFunc].
// Kept unexported so the signature can evolve freely.
type annotationHandler interface {
	validate(e AnnotationEvent) []FieldViolation
}

// AnnotationEvent is passed to [AnnotationFieldFunc] handlers.
// It is the stable public contract for annotation implementations.
//
// Args holds the atom-string arguments from the validate= expression.
// For zero-argument atoms (e.g. required) Args is nil.
// For single-argument calls (e.g. min(1)) Args is ["1"].
// For multi-argument calls (e.g. oneof(a b c)) Args is ["a", "b", "c"].
type AnnotationEvent struct {
	Value  any
	Path   string
	Args   []string // expression arguments; nil for atoms/zero-arg calls
	Exists bool
}

// AnnotationFieldFunc is a field-scoped annotation handler.
// The framework resolves the field value and passes it via [AnnotationEvent],
// so implementations do not need to navigate the raw config map.
//
// Returned [FieldViolation]s have their Paths set automatically to the field's path,
// matching the ergonomics of [Validator.AddValidator].
//
// Pass AnnotationFieldFunc values to [Registry.Register] directly.
type AnnotationFieldFunc func(AnnotationEvent) []FieldViolation

func (f AnnotationFieldFunc) validate(e AnnotationEvent) []FieldViolation { return f(e) }

// ── Annotation param helpers ──────────────────────────────────────────────────

// ParseParamInt parses an annotation parameter as a base-10 integer.
// Returns (0, false) if param is empty or not a valid integer.
func ParseParamInt(param string) (int64, bool) {
	if param == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(param, 10, 64)
	return n, err == nil
}

// ParseParamBool parses an annotation parameter as a boolean (case-insensitive).
//
// Recognized true values:  "true", "1", "yes"
// Recognized false values: "false", "0", "no"
//
// Returns (false, false) for empty or unrecognized values.
func ParseParamBool(param string) (value, ok bool) {
	switch strings.ToLower(param) {
	case "true", "1", "yes":
		return true, true
	case "false", "0", "no":
		return false, true
	}
	return false, false
}

// ParseParamList parses a pipe-separated ("|") annotation parameter into a slice.
// Returns nil if param is empty.
//
// Example: "a|b|c" → ["a", "b", "c"].
func ParseParamList(param string) []string {
	if param == "" {
		return nil
	}
	return strings.Split(param, "|")
}

// ── Cross validators ──────────────────────────────────────────────────────────

// Func is a per-key validation function registered via [Validator.AddValidator].
// It receives the value and path via [Event] and returns zero or more violations.
// Returned violations have their path set automatically by the framework.
type Func func(Event) []FieldViolation

// RuleValidator is a cross-key validator built with [Rule].
// Obtain one exclusively via [Rule]; the interface is sealed — no external implementations
// are permitted. Pass a RuleValidator to [Validator.AddRule].
type RuleValidator interface{ ruleValidator() }

type ruleValidatorImpl struct {
	run   func(*kongfig.Kongfig) []FieldViolation
	paths []string
}

func (ruleValidatorImpl) ruleValidator() {}

// Rule builds a RuleValidator that decodes the Kongfig into T via [kongfig.Get],
// then calls fn. Each field in T must use its kongfig tag as a full dot-delimited path
// from the config root — no [kongfig.At] prefix is accepted or needed.
//
//	type DBRule struct {
//	    MinConns int `kongfig:"db.min_conns"`
//	    MaxConns int `kongfig:"db.max_conns"`
//	}
//
// Paths reported on violations are inferred from T's leaf tags.
func Rule[T any](fn func(T) []FieldViolation) RuleValidator {
	return ruleValidatorImpl{
		paths: extractLeafPaths[T](""),
		run: func(k *kongfig.Kongfig) []FieldViolation {
			v, err := kongfig.Get[T](k)
			if err != nil {
				return []FieldViolation{{
					Message:  err.Error(),
					Code:     "kongfig.decode_error",
					Severity: SeverityError,
				}}
			}
			return fn(v)
		},
	}
}

// ── Schema ────────────────────────────────────────────────────────────────────

// schemaField holds the extracted path and the parsed validate= expression for one struct field.
// expr is nil when the field carries no validate= annotation.
type schemaField struct {
	expr *vexpr.Expr
	path string
}

// SchemaValidator is a schema handle built with [Schema] or [ForEach].
// Obtain one exclusively via those constructors; the interface is sealed — no external
// implementations are permitted. Pass a SchemaValidator to [Validator.AddSchema].
type SchemaValidator interface{ schemaValidator() }

type schemaValidatorImpl struct {
	fields []schemaField
	opts   []kongfig.GetOption
}

func (schemaValidatorImpl) schemaValidator() {}

// Schema extracts paths and validation annotations from T's kongfig struct tags.
// Use At() to scope to a sub-tree prefix.
func Schema[T any](opts ...kongfig.GetOption) SchemaValidator {
	return schemaValidatorImpl{
		fields: extractAnnotatedFields[T](kongfig.GetOptionsPath(opts)),
		opts:   opts,
	}
}

// forEachValidatorImpl applies Schema[T] annotations to each element of a slice
// or map at a fixed prefix in the config tree.
type forEachValidatorImpl struct {
	prefix string        // config path to the collection, e.g. "dbs"
	fields []schemaField // relative paths within each element (no prefix)
}

func (forEachValidatorImpl) schemaValidator() {}

// ForEach applies Schema[T] validation annotations to every element of the slice
// or map at prefix in the config tree.
//
// For slices the synthesized paths are prefix.0.field, prefix.1.field, …
// For maps the synthesized paths are prefix.key.field, …
//
//	v.AddSchema(validation.ForEach[DB]("dbs"))            // []DB
//	v.AddSchema(validation.ForEach[DB]("connections"))    // map[string]DB
func ForEach[T any](prefix string) SchemaValidator {
	return forEachValidatorImpl{
		prefix: prefix,
		fields: extractAnnotatedFields[T](""),
	}
}

// ── Validator options ─────────────────────────────────────────────────────────

// ValidatorOption configures a [Validator] at construction time.
// Built-in options are [WithNotifyOnLoad] and [WithValidateOnLoad].
type ValidatorOption interface {
	applyValidator(*Validator)
}

type fieldValidatorEntry struct {
	fn   Func
	path string
}

type notifyOnLoadOpt struct{}

func (notifyOnLoadOpt) applyValidator(v *Validator) { v.notifyOnLoad = true }

// WithNotifyOnLoad fires all field validators on each Load() via the OnLoad hook.
// Violations are accumulated in [Diagnostics.LoadViolations] and never reject a Load.
// Call [Validator.Validate] on the final merged config to surface errors.
func WithNotifyOnLoad() ValidatorOption { return notifyOnLoadOpt{} }

type validateOnLoadOpt struct{ at Severity }

func (o validateOnLoadOpt) applyValidator(v *Validator) {
	v.validateOnLoad = true
	v.onLoadCutoff = o.at
}

// WithValidateOnLoad fires all field validators on each Load() via the OnLoad hook.
// Loads that produce violations at severity at or above (i.e. more severe than) at are
// rejected immediately; the rest are accumulated in [Diagnostics.LoadViolations].
//
// Common usage: WithValidateOnLoad(SeverityError) rejects any load with error-level violations.
func WithValidateOnLoad(at Severity) ValidatorOption { return validateOnLoadOpt{at: at} }

// ── Validator ─────────────────────────────────────────────────────────────────

// Validator is the central validation object.
// Create with one of the constructor functions, configure, then Register with a
// *kongfig.Kongfig and/or call Validate.
type Validator struct {
	registry        *Registry
	registered      map[*kongfig.Kongfig]struct{}
	fieldValidators []fieldValidatorEntry
	rules           []ruleValidatorImpl
	schemas         []SchemaValidator
	loadViolations  []LayerViolation
	mu              sync.Mutex
	notifyOnLoad    bool
	validateOnLoad  bool
	onLoadCutoff    Severity
}

// newValidator is the internal constructor shared by all public constructors.
func newValidator(reg *Registry, opts ...ValidatorOption) *Validator {
	v := &Validator{registry: reg}
	for _, o := range opts {
		o.applyValidator(v)
	}
	return v
}

// NewWith creates a Validator that holds a live reference to reg.
// Annotations added to reg after this call are visible on the next [Validator.Validate] call.
// Returns an error if reg is nil — use [NewEmpty] for a validator with no annotation handlers.
func NewWith(reg *Registry, opts ...ValidatorOption) (*Validator, error) {
	if reg == nil {
		return nil, errors.New("validation.NewWith: reg is nil — use NewEmpty() for a validator with no annotation handlers")
	}
	return newValidator(reg, opts...), nil
}

// NewFrom creates a Validator with a snapshot copy of reg's current handlers.
// Changes to reg after this call are not reflected in the validator.
// Returns an error if reg is nil — use [NewEmpty] for a validator with no annotation handlers.
func NewFrom(reg *Registry, opts ...ValidatorOption) (*Validator, error) {
	if reg == nil {
		return nil, errors.New("validation.NewFrom: reg is nil — use NewEmpty() for a validator with no annotation handlers")
	}
	return newValidator(reg.clone(), opts...), nil
}

// NewWithDefaults creates a Validator that holds a live reference to [DefaultRegistry].
// Annotations registered on DefaultRegistry after this call are visible on the next
// [Validator.Validate] call.
func NewWithDefaults(opts ...ValidatorOption) *Validator {
	return newValidator(defaultRegistry, opts...)
}

// NewFromDefaults creates a Validator with a snapshot copy of [DefaultRegistry]'s current
// handlers. Changes to DefaultRegistry after this call are not reflected in the validator.
func NewFromDefaults(opts ...ValidatorOption) *Validator {
	return newValidator(defaultRegistry.clone(), opts...)
}

// NewEmpty creates a Validator with no annotation handlers.
// Schema annotations produce "unknown annotation" violations at [Validator.Validate] time.
func NewEmpty(opts ...ValidatorOption) *Validator {
	return newValidator(nil, opts...)
}

// Compile checks that every annotation tag in all registered schemas has a
// corresponding handler in the effective registry. Returns an error listing
// any unknown tags.
//
// Call after all [Validator.AddSchema] calls and before any [Validator.Validate]
// or [Validator.Register] calls to catch misspelled tag names at startup.
func (v *Validator) Compile() error {
	v.mu.Lock()
	schemas := make([]SchemaValidator, len(v.schemas))
	copy(schemas, v.schemas)
	fieldValidators := make([]fieldValidatorEntry, len(v.fieldValidators))
	copy(fieldValidators, v.fieldValidators)
	v.mu.Unlock()

	var errs []string

	// Validate paths registered via AddValidator (may be dynamic, provider-derived).
	for _, fv := range fieldValidators {
		if err := validatePathSyntax(fv.path); err != nil {
			errs = append(errs, err.Error())
		}
	}

	// Validate ForEach prefixes (also user-supplied strings).
	for _, sv := range schemas {
		if fe, ok := sv.(forEachValidatorImpl); ok {
			if err := validatePathSyntax(fe.prefix); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}

	// Check that every annotation name in each field's validate= expression
	// has a registered handler (or is a built-in combinator).
	reg := v.registry
	for _, sv := range schemas {
		for _, field := range schemaFields(sv) {
			if field.expr == nil {
				continue
			}
			errs = append(errs, compileExpr(*field.expr, reg, field.path)...)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation.Compile: %s", strings.Join(errs, "; "))
	}
	return nil
}

// schemaFields returns the field list for a SchemaValidator for use by Compile.
// forEachValidatorImpl fields are relative (no prefix) — Compile checks annotation
// tag names only, so the path prefix doesn't matter.
func schemaFields(sv SchemaValidator) []schemaField {
	switch s := sv.(type) {
	case schemaValidatorImpl:
		return s.fields
	case forEachValidatorImpl:
		return s.fields
	default:
		return nil
	}
}

// AddValidator registers a per-key validator for the given dot-delimited path.
func (v *Validator) AddValidator(path string, fn Func) {
	v.fieldValidators = append(v.fieldValidators, fieldValidatorEntry{path: path, fn: fn})
}

// AddRule registers a cross-key rule built with Rule.
func (v *Validator) AddRule(r RuleValidator) {
	impl, ok := r.(ruleValidatorImpl)
	if !ok {
		panic("validation: AddRule: unknown RuleValidator implementation")
	}
	v.rules = append(v.rules, impl)
}

// AddSchema stores the schema for lazy resolution at Validate() time.
// Annotation handlers in the effective registry are resolved when Validate() runs,
// so call order between AddSchema and registry.Register does not matter.
// Unknown annotation tags produce a SeverityError violation at Validate() time.
// Accepts both [Schema] and [ForEach] validators.
func (v *Validator) AddSchema(s SchemaValidator) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.schemas = append(v.schemas, s)
}

// Register wires the validator into k via an OnLoad hook.
// Only installs the hook when [WithNotifyOnLoad] or [WithValidateOnLoad] was set.
// Calling Register on the same k more than once is a no-op after the first call.
//
// Hook semantics depend on the option used at construction:
//   - [WithNotifyOnLoad]: all violations accumulate in [Diagnostics.LoadViolations]; Load never rejected.
//   - [WithValidateOnLoad]: violations at or above the cutoff severity reject the Load immediately.
func (v *Validator) Register(k *kongfig.Kongfig) {
	if !v.notifyOnLoad && !v.validateOnLoad {
		return
	}
	v.mu.Lock()
	if _, already := v.registered[k]; already {
		v.mu.Unlock()
		return
	}
	if v.registered == nil {
		v.registered = make(map[*kongfig.Kongfig]struct{})
	}
	v.registered[k] = struct{}{}
	v.mu.Unlock()

	k.OnLoad(func(e kongfig.LoadEvent) kongfig.LoadResult {
		violations := v.collectLoadViolations(e)
		if len(violations) == 0 {
			return kongfig.LoadResult{}
		}
		lv := LayerViolation{Layer: e.Layer, Violations: violations}
		if v.validateOnLoad {
			var errMsgs []string
			for _, violation := range violations {
				if violation.Severity <= v.onLoadCutoff {
					errMsgs = append(errMsgs, formatViolation(violation, lv.Layer.Meta.Name))
				}
			}
			if len(errMsgs) > 0 {
				// Violations at/above cutoff reject the load; don't accumulate since
				// the layer will not be committed.
				return kongfig.LoadResult{Err: fmt.Errorf("validation: %s", strings.Join(errMsgs, "; "))}
			}
		}
		// Non-rejected violations accumulate for Validate() to surface.
		v.mu.Lock()
		v.loadViolations = append(v.loadViolations, lv)
		v.mu.Unlock()
		return kongfig.LoadResult{}
	})
}

// collectLoadViolations runs per-load field validators for keys present in
// e.Layer.Data, checking values against e.ProposedData (the merged state after
// this load). Only paths contributed by the current layer are checked, so a
// stale invalid value from a previous layer cannot cause an unrelated load to fail.
func (v *Validator) collectLoadViolations(e kongfig.LoadEvent) []Violation {
	layerSrc := &kongfig.SourceMeta{Layer: e.Layer.Meta}
	var violations []Violation
	for _, fv := range v.fieldValidators {
		// Only fire if this path was contributed by the current layer.
		if _, inLayer := getNestedValue(e.Layer.Data, fv.path); !inLayer {
			continue
		}
		val, exists := getNestedValue(e.ProposedData, fv.path)
		if !exists {
			continue
		}
		for _, fviol := range fv.fn(Event{Key: fv.path, Value: val}) {
			violations = append(violations, Violation{
				Paths:    []PathSource{{Path: fv.path, Source: layerSrc}},
				Message:  fviol.Message,
				Code:     fviol.Code,
				Severity: fviol.Severity,
			})
		}
	}
	return violations
}

// Validate runs all validators against the current merged state of k and
// drains any per-load violations accumulated since the last Validate call.
// Returns nil if there are no violations of any severity.
func (v *Validator) Validate(k *kongfig.Kongfig) *Diagnostics {
	v.mu.Lock()
	loadViolations := v.loadViolations
	v.loadViolations = nil
	v.mu.Unlock()

	data := k.All()
	var violations []Violation

	// sourceFor looks up provenance for path and returns a *SourceMeta (nil if unknown).
	sourceFor := func(path string) *kongfig.SourceMeta {
		if sm, ok := k.SourceFor(path); ok {
			return &sm
		}
		return nil
	}

	// Field validators.
	for _, fv := range v.fieldValidators {
		val, exists := getNestedValue(data, fv.path)
		if !exists {
			continue
		}
		src := sourceFor(fv.path)
		for _, fviol := range fv.fn(Event{Key: fv.path, Value: val}) {
			violations = append(violations, Violation{
				Paths:    []PathSource{{Path: fv.path, Source: src}},
				Message:  fviol.Message,
				Code:     fviol.Code,
				Severity: fviol.Severity,
			})
		}
	}

	violations = append(violations, v.runSchemas(data, sourceFor)...)
	violations = append(violations, v.runRules(k, sourceFor)...)

	if len(violations) == 0 && len(loadViolations) == 0 {
		return nil
	}
	return &Diagnostics{Violations: violations, LoadViolations: loadViolations}
}

// runSchemas runs schema annotation validators against data.
// Lazily resolved — call order of AddSchema and Registry.Register does not matter;
// unknown tags surface as SeverityError violations.
func (v *Validator) runSchemas(data kongfig.ConfigData, sourceFor func(string) *kongfig.SourceMeta) []Violation {
	reg := v.registry
	var out []Violation
	for _, sv := range v.schemas {
		switch impl := sv.(type) {
		case schemaValidatorImpl:
			for _, field := range impl.fields {
				out = append(out, runFieldAnnotations(reg, data, field.path, field, sourceFor)...)
			}
		case forEachValidatorImpl:
			out = append(out, runForEachSchema(reg, data, impl, sourceFor)...)
		}
	}
	return out
}

// runForEachSchema runs schema annotations for every element of the collection at schema.prefix.
// Dispatches on []any (slice) or map[string]any / kongfig.ConfigData (map).
func runForEachSchema(reg *Registry, data kongfig.ConfigData, impl forEachValidatorImpl, sourceFor func(string) *kongfig.SourceMeta) []Violation {
	raw, exists := getNestedValue(data, impl.prefix)
	if !exists {
		return nil
	}
	var out []Violation
	switch coll := raw.(type) {
	case []any:
		for i := range coll {
			elemPrefix := impl.prefix + "[" + strconv.Itoa(i) + "]"
			out = append(out, runForEachFields(reg, data, elemPrefix, impl.fields, sourceFor)...)
		}
	case kongfig.ConfigData:
		for key := range coll {
			elemPrefix := impl.prefix + "[" + key + "]"
			out = append(out, runForEachFields(reg, data, elemPrefix, impl.fields, sourceFor)...)
		}
	}
	return out
}

// runForEachFields applies schema field annotations for all fields in a single collection element.
func runForEachFields(reg *Registry, data kongfig.ConfigData, elemPrefix string, fields []schemaField, sourceFor func(string) *kongfig.SourceMeta) []Violation {
	var out []Violation
	for _, field := range fields {
		fullPath := buildPath(elemPrefix, field.path)
		out = append(out, runFieldAnnotations(reg, data, fullPath, field, sourceFor)...)
	}
	return out
}

// runFieldAnnotations evaluates the validate= expression for a single field at fullPath.
// field.path holds the relative path (from the schema definition); fullPath is the
// resolved path in data (used for value lookup and violation reporting).
func runFieldAnnotations(reg *Registry, data kongfig.ConfigData, fullPath string, field schemaField, sourceFor func(string) *kongfig.SourceMeta) []Violation {
	if field.expr == nil {
		return nil
	}
	src := sourceFor(fullPath)
	val, exists := getNestedValue(data, fullPath)
	event := AnnotationEvent{Value: val, Path: fullPath, Exists: exists}
	var out []Violation
	for _, fviol := range evalExpr(*field.expr, event, reg, data) {
		out = append(out, Violation{
			Paths:    []PathSource{{Path: fullPath, Source: src}},
			Message:  fviol.Message,
			Code:     fviol.Code,
			Severity: fviol.Severity,
		})
	}
	return out
}

// builtinCombinators is the set of names handled structurally by evalExpr,
// not looked up in the Registry.
var builtinCombinators = map[string]bool{
	"all": true, "any": true, "each": true, "keys": true,
}

// evalExpr recursively evaluates a validate= expression against event.
// Built-in combinators (all, any, each, keys) are handled structurally;
// all other names are looked up in the Registry.
func evalExpr(e vexpr.Expr, event AnnotationEvent, reg *Registry, data kongfig.ConfigData) []FieldViolation {
	switch e.Name {
	case "all":
		var out []FieldViolation
		for _, arg := range e.Args {
			out = append(out, evalExpr(arg, event, reg, data)...)
		}
		return out

	case "any":
		return evalAny(e.Args, event, reg, data)

	case "each":
		if len(e.Args) != 1 {
			return []FieldViolation{{Message: "each requires exactly one argument", Code: "kongfig.expr", Severity: SeverityError}}
		}
		return evalEach(e.Args[0], event, reg, data)

	case "keys":
		if len(e.Args) != 1 {
			return []FieldViolation{{Message: "keys requires exactly one argument", Code: "kongfig.expr", Severity: SeverityError}}
		}
		return evalKeys(e.Args[0], event, reg, data)

	default:
		return evalRegistryHandler(e, event, reg)
	}
}

// evalAny runs each arg against event and returns nil if any passes (zero violations).
// If all fail, returns a single "must match one of" violation.
func evalAny(args []vexpr.Expr, event AnnotationEvent, reg *Registry, data kongfig.ConfigData) []FieldViolation {
	var failed []string
	for _, arg := range args {
		if len(evalExpr(arg, event, reg, data)) == 0 {
			return nil
		}
		failed = append(failed, exprString(arg))
	}
	return []FieldViolation{{
		Message:  "must match at least one of: " + strings.Join(failed, ", "),
		Code:     "kongfig.any",
		Severity: SeverityError,
	}}
}

// evalEach applies expr to each element of the collection at event.Path.
// Supports []any (slice) and ConfigData (map).
func evalEach(expr vexpr.Expr, event AnnotationEvent, reg *Registry, data kongfig.ConfigData) []FieldViolation {
	var out []FieldViolation
	switch coll := event.Value.(type) {
	case []any:
		for i, elem := range coll {
			sub := AnnotationEvent{Value: elem, Path: fmt.Sprintf("%s[%d]", event.Path, i), Exists: true}
			out = append(out, evalExpr(expr, sub, reg, data)...)
		}
	case kongfig.ConfigData:
		for _, k := range sortedKeys(coll) {
			sub := AnnotationEvent{Value: coll[k], Path: fmt.Sprintf("%s[%s]", event.Path, k), Exists: true}
			out = append(out, evalExpr(expr, sub, reg, data)...)
		}
	}
	return out
}

// evalKeys applies expr to each key of the map at event.Path (key as string value).
func evalKeys(expr vexpr.Expr, event AnnotationEvent, reg *Registry, data kongfig.ConfigData) []FieldViolation {
	coll, ok := event.Value.(kongfig.ConfigData)
	if !ok {
		return nil
	}
	var out []FieldViolation
	for _, k := range sortedKeys(coll) {
		sub := AnnotationEvent{Value: k, Path: fmt.Sprintf("%s[%s]", event.Path, k), Exists: true}
		out = append(out, evalExpr(expr, sub, reg, data)...)
	}
	return out
}

// evalRegistryHandler dispatches a leaf expression node to its Registry handler.
// Args must all be atoms (non-call expressions); call-as-arg is a config error.
func evalRegistryHandler(e vexpr.Expr, event AnnotationEvent, reg *Registry) []FieldViolation {
	h, ok := reg.lookup(e.Name)
	if !ok {
		return []FieldViolation{{
			Message:  fmt.Sprintf("unknown annotation %q; register it via Registry.Register or RegisterAnnotation", e.Name),
			Code:     "kongfig.unknown_annotation",
			Severity: SeverityError,
		}}
	}
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		if a.IsCall() {
			return []FieldViolation{{
				Message:  fmt.Sprintf("annotation %q: nested call expressions are not supported as arguments", e.Name),
				Code:     "kongfig.expr",
				Severity: SeverityError,
			}}
		}
		args[i] = a.Name
	}
	if !e.IsCall() {
		args = nil // atom: no args passed
	}
	return h.validate(AnnotationEvent{Value: event.Value, Path: event.Path, Args: args, Exists: event.Exists})
}

// exprString returns a compact human-readable representation of an Expr node.
func exprString(e vexpr.Expr) string {
	if !e.IsCall() || len(e.Args) == 0 {
		return e.Name
	}
	parts := make([]string, len(e.Args))
	for i, a := range e.Args {
		parts[i] = exprString(a)
	}
	return fmt.Sprintf("%s(%s)", e.Name, strings.Join(parts, " "))
}

// compileExpr walks an Expr tree and returns errors for unknown annotation names.
// Built-in combinators are skipped; all other names are checked against reg.
func compileExpr(e vexpr.Expr, reg *Registry, path string) []string {
	if builtinCombinators[e.Name] {
		var errs []string
		for _, arg := range e.Args {
			errs = append(errs, compileExpr(arg, reg, path)...)
		}
		return errs
	}
	if _, ok := reg.lookup(e.Name); !ok {
		return []string{fmt.Sprintf("%s uses unknown annotation %q", path, e.Name)}
	}
	return nil
}

// sortedKeys returns the keys of m in sorted order.
func sortedKeys(m kongfig.ConfigData) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// runRules runs cross-key rules against k, attaching per-path source attribution.
func (v *Validator) runRules(k *kongfig.Kongfig, sourceFor func(string) *kongfig.SourceMeta) []Violation {
	var out []Violation
	for _, rule := range v.rules {
		paths := make([]PathSource, len(rule.paths))
		for i, p := range rule.paths {
			paths[i] = PathSource{Path: p, Source: sourceFor(p)}
		}
		for _, fviol := range rule.run(k) {
			out = append(out, Violation{
				Paths:    paths,
				Message:  fviol.Message,
				Code:     fviol.Code,
				Severity: fviol.Severity,
			})
		}
	}
	return out
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// parsePathSegment splits a path segment into its map key and optional bracket
// accessor. "dbs[0]" → ("dbs", "0", true); "host" → ("host", "", false).
func parsePathSegment(seg string) (mapKey, accessor string, hasBracket bool) {
	i := strings.IndexByte(seg, '[')
	if i < 0 {
		return seg, "", false
	}
	j := strings.LastIndexByte(seg, ']')
	if j <= i {
		return seg, "", false // malformed: treat as plain key
	}
	return seg[:i], seg[i+1 : j], true
}

// validatePathSyntax checks that every key segment in path does not contain
// reserved characters ('.', '[', ']'). Returns an error with the offending segment.
func validatePathSyntax(path string) error {
	for seg := range strings.SplitSeq(path, ".") {
		key, _, _ := parsePathSegment(seg)
		if err := schema.ValidateKeyName(key); err != nil {
			return fmt.Errorf("path %q: %w", path, err)
		}
	}
	return nil
}

// getNestedValue retrieves a value at the given dot-path from m.
// Segments use bracket notation for collection access: "dbs[0].host" (slice
// index) or "conns[primary].host" (map key). Integer accessors index into
// []any; non-integer accessors look up in ConfigData.
// Assumes the tree has been normalized via [kongfig.ToConfigData] so all map
// nodes are ConfigData.
func getNestedValue(m kongfig.ConfigData, path string) (any, bool) {
	var cur any = m
	for seg := range strings.SplitSeq(path, ".") {
		mapKey, accessor, hasBracket := parsePathSegment(seg)
		// Step 1: navigate the map key (must be ConfigData at this point).
		cd, ok := cur.(kongfig.ConfigData)
		if !ok {
			return nil, false
		}
		cur, ok = cd[mapKey]
		if !ok {
			return nil, false
		}
		if !hasBracket {
			continue
		}
		// Step 2: navigate the bracket accessor (slice index or nested map key).
		if idx, err := strconv.Atoi(accessor); err == nil {
			sl, ok := cur.([]any)
			if !ok || idx < 0 || idx >= len(sl) {
				return nil, false
			}
			cur = sl[idx]
		} else {
			cd2, ok := cur.(kongfig.ConfigData)
			if !ok {
				return nil, false
			}
			cur, ok = cd2[accessor]
			if !ok {
				return nil, false
			}
		}
	}
	return cur, true
}

// extractLeafPaths reflects on T and returns all leaf dot-paths using kongfig tags.
func extractLeafPaths[T any](prefix string) []string {
	var out []string
	collectLeafPaths(reflect.TypeFor[T](), prefix, &out)
	return out
}

func collectLeafPaths(typ reflect.Type, prefix string, out *[]string) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for field := range typ.Fields() {
		if field.Anonymous {
			collectLeafPaths(field.Type, prefix, out)
			continue
		}
		if !field.IsExported() {
			continue
		}
		ft := schema.ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if ft.Skip {
			continue
		}
		path := buildPath(prefix, ft.Name)
		if ft.Squash {
			collectLeafPaths(field.Type, prefix, out)
			continue
		}
		subTyp := field.Type
		for subTyp.Kind() == reflect.Pointer {
			subTyp = subTyp.Elem()
		}
		if subTyp.Kind() == reflect.Struct {
			collectLeafPaths(field.Type, path, out)
			continue
		}
		*out = append(*out, path)
	}
}

// extractAnnotatedFields reflects on T and returns fields with their validation annotations.
func extractAnnotatedFields[T any](prefix string) []schemaField {
	var out []schemaField
	collectAnnotatedFields(reflect.TypeFor[T](), prefix, &out)
	return out
}

func collectAnnotatedFields(typ reflect.Type, prefix string, out *[]schemaField) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for field := range typ.Fields() {
		if field.Anonymous {
			collectAnnotatedFields(field.Type, prefix, out)
			continue
		}
		if !field.IsExported() {
			continue
		}
		ft := schema.ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if ft.Skip {
			continue
		}
		path := buildPath(prefix, ft.Name)
		if ft.Squash {
			collectAnnotatedFields(field.Type, prefix, out)
			continue
		}
		subTyp := field.Type
		for subTyp.Kind() == reflect.Pointer {
			subTyp = subTyp.Elem()
		}
		if subTyp.Kind() == reflect.Struct {
			collectAnnotatedFields(field.Type, path, out)
			continue
		}
		if exprStr, ok := schema.ParseExtraValue(ft.Extras, "validate"); ok {
			e, err := vexpr.ParseExpr(exprStr)
			if err != nil {
				panic(fmt.Sprintf("kongfig: validate annotation on field %q: %v", ft.Name, err))
			}
			*out = append(*out, schemaField{path: path, expr: &e})
		}
	}
}

func buildPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// AsOption returns a [kongfig.Option] that registers v as the config validator
// on a [kongfig.Kongfig] instance. The kong resolver calls it automatically
// after parsing — no additional wiring required:
//
//	v := validation.NewWithDefaults()
//	kf := kongfig.NewFor[Config](v.AsOption())
//	k, _ := kong.New(cli, kong.Resolvers(resolver.New(kf)))
func (v *Validator) AsOption() kongfig.Option {
	return kongfig.WithValidator(&kongfigAdapter{v: v})
}

type kongfigAdapter struct{ v *Validator }

func (a *kongfigAdapter) ValidateConfig(k *kongfig.Kongfig) error {
	return a.v.Validate(k).Err()
}
