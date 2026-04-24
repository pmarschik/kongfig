package validation

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/pmarschik/kongfig/schema"
)

// ── Composite rule helpers ────────────────────────────────────────────────────
//
// Each function takes a pointer to a decoded struct *T and one or more pointers
// to fields within that struct (e.g. &c.Host, &c.Socket). Paths are derived
// automatically from the field's kongfig tag.
//
// Intended for use inside Rule[T] callbacks:
//
//	v.AddRule(validation.Rule(func(c TLSConfig) []validation.FieldViolation {
//	    return validation.AllOrNone(&c, &c.CertFile, &c.KeyFile)
//	}))
//
// All helpers panic if a pointer argument cannot be matched to a field in *T,
// since that indicates a programming error (wrong struct/field mismatch).

// ExactlyOneOf requires exactly one of the given fields to be non-zero.
func ExactlyOneOf[T any](v *T, ptrs ...any) []FieldViolation {
	fields := resolveFields(v, ptrs)
	paths := fieldPaths(fields)
	var set []string
	for _, f := range fields {
		if isFieldNonZero(f.value) {
			set = append(set, f.path)
		}
	}
	if len(set) == 1 {
		return nil
	}
	if len(set) == 0 {
		return []FieldViolation{{
			Message:  fmt.Sprintf("exactly one of [%s] must be set; none are set", strings.Join(paths, ", ")),
			Code:     "kongfig.exactly_one_of",
			Severity: SeverityError,
		}}
	}
	return []FieldViolation{{
		Message:  fmt.Sprintf("exactly one of [%s] must be set; got: %s", strings.Join(paths, ", "), strings.Join(set, ", ")),
		Code:     "kongfig.exactly_one_of",
		Severity: SeverityError,
	}}
}

// AtLeastOneOf requires at least one of the given fields to be non-zero.
func AtLeastOneOf[T any](v *T, ptrs ...any) []FieldViolation {
	fields := resolveFields(v, ptrs)
	for _, f := range fields {
		if isFieldNonZero(f.value) {
			return nil
		}
	}
	return []FieldViolation{{
		Message:  fmt.Sprintf("at least one of [%s] must be set", strings.Join(fieldPaths(fields), ", ")),
		Code:     "kongfig.at_least_one_of",
		Severity: SeverityError,
	}}
}

// MutuallyExclusive requires at most one of the given fields to be non-zero.
func MutuallyExclusive[T any](v *T, ptrs ...any) []FieldViolation {
	fields := resolveFields(v, ptrs)
	var set []string
	for _, f := range fields {
		if isFieldNonZero(f.value) {
			set = append(set, f.path)
		}
	}
	if len(set) <= 1 {
		return nil
	}
	return []FieldViolation{{
		Message:  fmt.Sprintf("at most one of [%s] may be set; got: %s", strings.Join(fieldPaths(fields), ", "), strings.Join(set, ", ")),
		Code:     "kongfig.mutually_exclusive",
		Severity: SeverityError,
	}}
}

// AllOrNone requires either all of the given fields to be non-zero, or none of them.
func AllOrNone[T any](v *T, ptrs ...any) []FieldViolation {
	fields := resolveFields(v, ptrs)
	var set, unset []string
	for _, f := range fields {
		if isFieldNonZero(f.value) {
			set = append(set, f.path)
		} else {
			unset = append(unset, f.path)
		}
	}
	if len(set) == 0 || len(unset) == 0 {
		return nil
	}
	return []FieldViolation{{
		Message: fmt.Sprintf(
			"either all or none of [%s] must be set; set: %s, unset: %s",
			strings.Join(fieldPaths(fields), ", "),
			strings.Join(set, ", "),
			strings.Join(unset, ", "),
		),
		Code:     "kongfig.all_or_none",
		Severity: SeverityError,
	}}
}

// RequiredWith requires field to be non-zero when any of the trigger fields are non-zero.
func RequiredWith[T any](v *T, field any, triggers ...any) []FieldViolation {
	fInfos := resolveFields(v, []any{field})
	f := fInfos[0]
	tFields := resolveFields(v, triggers)
	for _, t := range tFields {
		if isFieldNonZero(t.value) {
			if !isFieldNonZero(f.value) {
				return []FieldViolation{{
					Message:  fmt.Sprintf("%s is required when any of [%s] is set", f.path, strings.Join(fieldPaths(tFields), ", ")),
					Code:     "kongfig.required_with",
					Severity: SeverityError,
				}}
			}
			return nil
		}
	}
	return nil
}

// RequiredWithout requires field to be non-zero when none of the fallback fields are non-zero.
func RequiredWithout[T any](v *T, field any, fallbacks ...any) []FieldViolation {
	fInfos := resolveFields(v, []any{field})
	f := fInfos[0]
	fbFields := resolveFields(v, fallbacks)
	for _, fb := range fbFields {
		if isFieldNonZero(fb.value) {
			return nil
		}
	}
	if !isFieldNonZero(f.value) {
		return []FieldViolation{{
			Message:  fmt.Sprintf("%s is required when none of [%s] are set", f.path, strings.Join(fieldPaths(fbFields), ", ")),
			Code:     "kongfig.required_without",
			Severity: SeverityError,
		}}
	}
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// ruleFieldInfo is the resolved kongfig path + reflected value for a pointer argument.
type ruleFieldInfo struct {
	path  string
	value reflect.Value
}

// resolveFields resolves each ptr in ptrs against the struct pointed to by v,
// returning the kongfig path and reflected value for each.
// Panics if a ptr cannot be matched to a field in *T.
func resolveFields[T any](v *T, ptrs []any) []ruleFieldInfo {
	sval := reflect.ValueOf(v).Elem()
	styp := reflect.TypeFor[T]()
	out := make([]ruleFieldInfo, len(ptrs))
	for i, ptr := range ptrs {
		pval := reflect.ValueOf(ptr)
		if pval.Kind() != reflect.Pointer || pval.IsNil() {
			panic(fmt.Sprintf("kongfig/validation: rule helper argument %d must be a non-nil pointer to a field of %T", i, v))
		}
		targetAddr := pval.Pointer()
		path, found := walkForPtr(sval, styp, "", targetAddr)
		if !found {
			panic(fmt.Sprintf("kongfig/validation: rule helper argument %d not found in %T; pass &v.FieldName, not a copy", i, v))
		}
		out[i] = ruleFieldInfo{path: path, value: pval.Elem()}
	}
	return out
}

// walkForPtr recursively searches structVal (of structType) for a field whose
// address equals targetAddr, returning the kongfig-derived path when found.
// Handles embedded structs, squash, and skipped fields via kongfig tags.
func walkForPtr(structVal reflect.Value, structType reflect.Type, prefix string, targetAddr uintptr) (string, bool) {
	structVal, structType, ok := derefPtr(structVal, structType)
	if !ok || structType.Kind() != reflect.Struct {
		return "", false
	}
	for i := range structType.NumField() {
		if path, found := walkStructField(structType.Field(i), structVal.Field(i), prefix, targetAddr); found {
			return path, true
		}
	}
	return "", false
}

// walkStructField processes one struct field during the pointer-address search.
func walkStructField(sf reflect.StructField, fv reflect.Value, prefix string, targetAddr uintptr) (string, bool) {
	if sf.Anonymous {
		return walkForPtr(fv, sf.Type, prefix, targetAddr)
	}
	if !sf.IsExported() {
		return "", false
	}
	ft := schema.ParseFieldTag(sf.Tag.Get("kongfig"), sf.Name)
	if ft.Skip {
		return "", false
	}
	if ft.Squash {
		return walkForPtr(fv, sf.Type, prefix, targetAddr)
	}
	path := buildPath(prefix, ft.Name)
	if fv.CanAddr() && fv.Addr().Pointer() == targetAddr {
		return path, true
	}
	subTyp := sf.Type
	for subTyp.Kind() == reflect.Pointer {
		subTyp = subTyp.Elem()
	}
	if subTyp.Kind() == reflect.Struct {
		return walkForPtr(fv, sf.Type, path, targetAddr)
	}
	return "", false
}

// derefPtr unwraps pointer types in both val and typ until a non-pointer is reached.
// Returns (val, typ, false) if a nil pointer is encountered mid-chain.
func derefPtr(val reflect.Value, typ reflect.Type) (reflect.Value, reflect.Type, bool) {
	for typ.Kind() == reflect.Pointer {
		if val.IsNil() {
			return val, typ, false
		}
		typ = typ.Elem()
		val = val.Elem()
	}
	return val, typ, true
}

func isFieldNonZero(v reflect.Value) bool { return v.IsValid() && !v.IsZero() }

func fieldPaths(fields []ruleFieldInfo) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.path
	}
	return out
}
