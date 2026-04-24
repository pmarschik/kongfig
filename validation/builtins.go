package validation

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// init registers all built-in annotation handlers into defaultRegistry.
// This runs before any user code; the registry is not yet shared so no lock needed.
func init() {
	for name, fn := range builtinAnnotations {
		defaultRegistry.handlers[name] = fn
	}
}

// builtinAnnotations is the canonical set of built-in registry handlers.
//
// Built-in annotations (registered in [DefaultRegistry]):
//
//   - required          — field must be present
//   - notempty          — string must be present and non-empty
//   - min(N)            — numeric: value ≥ N; string/slice: length ≥ N
//   - max(N)            — numeric: value ≤ N; string/slice: length ≤ N
//   - len(N)            — string/slice: exact length == N
//   - oneof(a b c)      — string value must be one of the space-separated options
//   - pattern(re)       — string must match the regular expression (single-quoted arg)
//   - email             — string must be a syntactically valid e-mail address
//   - url               — string must be a URL with a non-empty scheme and host
//   - hostname          — string must be a valid RFC 1123 DNS hostname
//   - ip                — string must be a valid IPv4 or IPv6 address
//   - ipv4              — string must be a valid IPv4 address
//   - ipv6              — string must be a valid IPv6 address
//   - port              — numeric value must be in range 1–65535
//   - file              — string must be a path to an existing regular file
//   - dir               — string must be a path to an existing directory
//   - exists            — string must be a path to an existing file or directory
//
// In addition, the following combinators are built into the evaluator itself
// (not in this map) and therefore cannot be overridden via the registry:
//
//   - all(e1 e2 ...)    — conjunction: passes if every sub-expression passes
//   - any(e1 e2 ...)    — disjunction: passes if at least one sub-expression passes
//   - each(e)           — applies e to every element of a slice or map value
//   - keys(e)           — applies e to every key of a map value
var builtinAnnotations = map[string]AnnotationFieldFunc{
	"required": builtinRequired,
	"notempty": builtinNotEmpty,
	"min":      builtinMin,
	"max":      builtinMax,
	"len":      builtinLen,
	"oneof":    builtinOneOf,
	"pattern":  builtinPattern,
	"email":    builtinEmail,
	"url":      builtinURL,
	"hostname": builtinHostname,
	"ip":       builtinIP,
	"ipv4":     builtinIPv4,
	"ipv6":     builtinIPv6,
	"port":     builtinPort,
	"file":     builtinFile,
	"dir":      builtinDir,
	"exists":   builtinExists,
}

// onlyArg returns the single argument from a one-argument annotation.
// Panics if more than one argument is present (a programming error in the validate= expression).
// Returns "" when no argument is present (e.g. the annotation is used as a bare atom).
func onlyArg(e AnnotationEvent) string {
	if len(e.Args) > 1 {
		panic(fmt.Sprintf("kongfig/validation: annotation %q expects at most one argument, got %d", e.Path, len(e.Args)))
	}
	if len(e.Args) == 1 {
		return e.Args[0]
	}
	return ""
}

// ── required ──────────────────────────────────────────────────────────────────

func builtinRequired(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return []FieldViolation{{
			Message:  "required field is missing",
			Code:     "kongfig.required",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── min / max ─────────────────────────────────────────────────────────────────

func builtinMin(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	if n, ok := toInt64(e.Value); ok {
		limit, err := strconv.ParseInt(onlyArg(e), 10, 64)
		if err != nil {
			return nil // bad param — caught by Compile
		}
		if n < limit {
			return []FieldViolation{{
				Message:  fmt.Sprintf("value %d is less than minimum %d", n, limit),
				Code:     "kongfig.min",
				Severity: SeverityError,
			}}
		}
		return nil
	}
	if n, ok := toUint64(e.Value); ok {
		limit, err := strconv.ParseUint(onlyArg(e), 10, 64)
		if err != nil {
			return nil
		}
		if n < limit {
			return []FieldViolation{{
				Message:  fmt.Sprintf("value %d is less than minimum %d", n, limit),
				Code:     "kongfig.min",
				Severity: SeverityError,
			}}
		}
		return nil
	}
	limit, err := strconv.ParseFloat(onlyArg(e), 64)
	if err != nil {
		return nil // bad param — caught by Compile
	}
	if n, ok := toFloat64(e.Value); ok {
		if n < limit {
			return []FieldViolation{{
				Message:  fmt.Sprintf("value %g is less than minimum %g", n, limit),
				Code:     "kongfig.min",
				Severity: SeverityError,
			}}
		}
		return nil
	}
	if l, ok := lenOf(e.Value); ok {
		if l < int(limit) {
			return []FieldViolation{{
				Message:  fmt.Sprintf("length %d is less than minimum %d", l, int(limit)),
				Code:     "kongfig.min",
				Severity: SeverityError,
			}}
		}
	}
	return nil
}

func builtinMax(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	if n, ok := toInt64(e.Value); ok {
		limit, err := strconv.ParseInt(onlyArg(e), 10, 64)
		if err != nil {
			return nil
		}
		if n > limit {
			return []FieldViolation{{
				Message:  fmt.Sprintf("value %d exceeds maximum %d", n, limit),
				Code:     "kongfig.max",
				Severity: SeverityError,
			}}
		}
		return nil
	}
	if n, ok := toUint64(e.Value); ok {
		limit, err := strconv.ParseUint(onlyArg(e), 10, 64)
		if err != nil {
			return nil
		}
		if n > limit {
			return []FieldViolation{{
				Message:  fmt.Sprintf("value %d exceeds maximum %d", n, limit),
				Code:     "kongfig.max",
				Severity: SeverityError,
			}}
		}
		return nil
	}
	limit, err := strconv.ParseFloat(onlyArg(e), 64)
	if err != nil {
		return nil
	}
	if n, ok := toFloat64(e.Value); ok {
		if n > limit {
			return []FieldViolation{{
				Message:  fmt.Sprintf("value %g exceeds maximum %g", n, limit),
				Code:     "kongfig.max",
				Severity: SeverityError,
			}}
		}
		return nil
	}
	if l, ok := lenOf(e.Value); ok {
		if l > int(limit) {
			return []FieldViolation{{
				Message:  fmt.Sprintf("length %d exceeds maximum %d", l, int(limit)),
				Code:     "kongfig.max",
				Severity: SeverityError,
			}}
		}
	}
	return nil
}

// ── len ───────────────────────────────────────────────────────────────────────

func builtinLen(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	want, err := strconv.Atoi(onlyArg(e))
	if err != nil {
		return nil
	}
	l, ok := lenOf(e.Value)
	if !ok {
		return nil
	}
	if l != want {
		return []FieldViolation{{
			Message:  fmt.Sprintf("length %d does not equal required length %d", l, want),
			Code:     "kongfig.len",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── oneof ─────────────────────────────────────────────────────────────────────

func builtinOneOf(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok {
		return nil
	}
	if slices.Contains(e.Args, s) {
		return nil
	}
	return []FieldViolation{{
		Message:  fmt.Sprintf("value %q is not one of: %s", s, strings.Join(e.Args, ", ")),
		Code:     "kongfig.oneof",
		Severity: SeverityError,
	}}
}

// ── pattern ───────────────────────────────────────────────────────────────────

func builtinPattern(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok {
		return nil
	}
	matched, err := regexp.MatchString("^(?:"+onlyArg(e)+")$", s)
	if err != nil || !matched {
		return []FieldViolation{{
			Message:  fmt.Sprintf("value %q does not match pattern %q", s, onlyArg(e)),
			Code:     "kongfig.pattern",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── email ─────────────────────────────────────────────────────────────────────

// emailRegex is a pragmatic e-mail syntax check: local@domain.tld, no whitespace.
var emailRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func builtinEmail(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok || !emailRegex.MatchString(s) {
		return []FieldViolation{{
			Message:  fmt.Sprintf("value %q is not a valid e-mail address", s),
			Code:     "kongfig.email",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── url ───────────────────────────────────────────────────────────────────────

func builtinURL(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return []FieldViolation{{
			Message:  fmt.Sprintf("value %q is not a valid URL (scheme and host required)", s),
			Code:     "kongfig.url",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── hostname ──────────────────────────────────────────────────────────────────

// hostnameRegex validates RFC 1123 DNS hostnames (labels up to 63 chars, separated by dots).
var hostnameRegex = regexp.MustCompile(
	`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?` +
		`(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`,
)

func builtinHostname(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok || !hostnameRegex.MatchString(s) {
		return []FieldViolation{{
			Message:  fmt.Sprintf("value %q is not a valid hostname", s),
			Code:     "kongfig.hostname",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── notempty ──────────────────────────────────────────────────────────────────

func builtinNotEmpty(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return []FieldViolation{{
			Message:  "required field is missing",
			Code:     "kongfig.notempty",
			Severity: SeverityError,
		}}
	}
	s, ok := e.Value.(string)
	if ok && s == "" {
		return []FieldViolation{{
			Message:  "value must not be empty",
			Code:     "kongfig.notempty",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── ip / ipv4 / ipv6 ─────────────────────────────────────────────────────────

func builtinIP(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok || net.ParseIP(s) == nil {
		return []FieldViolation{{
			Message:  fmt.Sprintf("value %q is not a valid IP address", e.Value),
			Code:     "kongfig.ip",
			Severity: SeverityError,
		}}
	}
	return nil
}

func builtinIPv4(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok {
		return nil
	}
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return []FieldViolation{{
			Message:  fmt.Sprintf("value %q is not a valid IPv4 address", s),
			Code:     "kongfig.ipv4",
			Severity: SeverityError,
		}}
	}
	return nil
}

func builtinIPv6(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok {
		return nil
	}
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() != nil {
		return []FieldViolation{{
			Message:  fmt.Sprintf("value %q is not a valid IPv6 address", s),
			Code:     "kongfig.ipv6",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── port ──────────────────────────────────────────────────────────────────────

func builtinPort(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	if n, ok := toInt64(e.Value); ok {
		if n >= 1 && n <= 65535 {
			return nil
		}
	} else if n, ok := toUint64(e.Value); ok {
		if n >= 1 && n <= 65535 {
			return nil
		}
	} else if n, ok := toFloat64(e.Value); ok && n == float64(int(n)) && n >= 1 && n <= 65535 {
		return nil
	}
	return []FieldViolation{{
		Message:  fmt.Sprintf("value %v is not a valid port (1–65535)", e.Value),
		Code:     "kongfig.port",
		Severity: SeverityError,
	}}
}

// ── file / dir / exists ───────────────────────────────────────────────────────

func builtinFile(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok {
		return nil
	}
	info, err := os.Stat(s)
	if err != nil || info.IsDir() {
		return []FieldViolation{{
			Message:  fmt.Sprintf("path %q is not an existing regular file", s),
			Code:     "kongfig.file",
			Severity: SeverityError,
		}}
	}
	return nil
}

func builtinDir(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok {
		return nil
	}
	info, err := os.Stat(s)
	if err != nil || !info.IsDir() {
		return []FieldViolation{{
			Message:  fmt.Sprintf("path %q is not an existing directory", s),
			Code:     "kongfig.dir",
			Severity: SeverityError,
		}}
	}
	return nil
}

func builtinExists(e AnnotationEvent) []FieldViolation {
	if !e.Exists {
		return nil
	}
	s, ok := e.Value.(string)
	if !ok {
		return nil
	}
	if _, err := os.Stat(s); err != nil {
		return []FieldViolation{{
			Message:  fmt.Sprintf("path %q does not exist", s),
			Code:     "kongfig.exists",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

// toInt64 converts signed integer types to int64.
// Returns (0, false) for non-signed-integer values (including float and uint types).
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// toUint64 converts unsigned integer types to uint64.
// Returns (0, false) for non-unsigned-integer values (including float and signed int types).
func toUint64(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint:
		return uint64(n), true
	case uint8:
		return uint64(n), true
	case uint16:
		return uint64(n), true
	case uint32:
		return uint64(n), true
	case uint64:
		return n, true
	}
	return 0, false
}

// toFloat64 converts float types to float64.
// Returns (0, false) for non-float values; use toInt64/toUint64 for integer types.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// lenOf returns the length of a string (bytes), slice, array, or map.
// Returns (0, false) for types that have no meaningful length.
func lenOf(v any) (int, bool) {
	if s, ok := v.(string); ok {
		return len(s), true
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() { //nolint:exhaustive // intentional: only slice/array/map have a meaningful length here
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len(), true
	}
	return 0, false
}
