package kongfig

import (
	"reflect"
	"time"
)

// EqualConfigData reports whether two configuration trees hold the same values,
// ignoring how each side spells them: a nested table may be [ConfigData] on one
// side and map[string]any on the other, a list []string or []any, a number int
// on one side and int64 on the other.
//
// It is the comparison [EditDocument] uses to check a rewritten document against
// the data it was asked to hold, where one side comes from a caller's Go values
// and the other from a parser. A nil tree equals an empty one.
func EqualConfigData(a, b ConfigData) bool {
	return equalMaps(a, b)
}

// EqualValues reports whether two values are the same value, comparing maps and
// lists structurally and numbers by the number they hold rather than by their Go
// type. [RenderedValue] is unwrapped first, so data that went through a render
// path compares against the plain value it wraps. Two times are the same when
// they name the same instant, whichever location they carry.
//
// Values it cannot tell apart safely compare unequal — an integer beyond the
// range float64 spells exactly is not equal to any float. Callers use this to
// verify that a change landed, and a false "same" there is worse than a false
// "different", which only costs the caller a fallback.
func EqualValues(a, b any) bool {
	a, b = unwrapRenderedValue(a), unwrapRenderedValue(b)
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if same, decided := equalContainers(a, b); decided {
		return same
	}
	if same, decided := equalScalars(a, b); decided {
		return same
	}
	if reflect.TypeOf(a).Comparable() && reflect.TypeOf(b).Comparable() {
		return a == b
	}
	return reflect.DeepEqual(a, b)
}

// equalContainers answers for the values that hold other values. A container is
// only ever the same value as a container of the same shape, so one side being
// one decides the question either way.
func equalContainers(a, b any) (same, decided bool) {
	am, aIsMap := asStringMap(a)
	bm, bIsMap := asStringMap(b)
	if aIsMap || bIsMap {
		return aIsMap && bIsMap && equalMaps(am, bm), true
	}
	as, aIsSlice := asAnySlice(a)
	bs, bIsSlice := asAnySlice(b)
	if aIsSlice || bIsSlice {
		return aIsSlice && bIsSlice && equalSlices(as, bs), true
	}
	return false, false
}

// equalScalars answers for the values whose Go type says less than the value in
// it: a time carries the location it was read in, a number its width.
func equalScalars(a, b any) (same, decided bool) {
	at, aIsTime := a.(time.Time)
	bt, bIsTime := b.(time.Time)
	if aIsTime || bIsTime {
		return aIsTime && bIsTime && at.Equal(bt), true
	}
	an, aIsNum := asNumber(a)
	bn, bIsNum := asNumber(b)
	if aIsNum || bIsNum {
		return aIsNum && bIsNum && equalNumbers(an, bn), true
	}
	return false, false
}

// unwrapRenderedValue returns the value a [RenderedValue] wraps, so render
// bookkeeping — redaction display, source, codec marks — does not count as part
// of the value.
func unwrapRenderedValue(v any) any {
	if rv, ok := v.(RenderedValue); ok {
		return rv.Value
	}
	return v
}

// equalMaps compares two trees key by key. A key holding nil is a key the
// document has, so it does not equal a missing one.
func equalMaps(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		bv, ok := b[key]
		if !ok || !EqualValues(av, bv) {
			return false
		}
	}
	return true
}

func equalSlices(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !EqualValues(a[i], b[i]) {
			return false
		}
	}
	return true
}

// asStringMap views v as a string-keyed map when it is one, whichever map type
// it was written as.
func asStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case ConfigData:
		return m, true
	case map[string]any:
		return m, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	out := make(map[string]any, rv.Len())
	for iter := rv.MapRange(); iter.Next(); {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out, true
}

// asAnySlice views v as a list when it is one. A string is not a list, and
// neither is a map.
func asAnySlice(v any) ([]any, bool) {
	if s, ok := v.([]any); ok {
		return s, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// numKind says which of a [number]'s fields holds the value.
type numKind uint8

const (
	numInt numKind = iota + 1
	numUint
	numFloat
)

// number is a numeric value read out of its Go type, so two numbers can be
// compared without going through a conversion that might lose something.
type number struct {
	i    int64
	u    uint64
	f    float64
	kind numKind
}

// maxExactInt is the largest integer float64 spells exactly. Past it a float
// stands for a range of integers, so an integer and a float compare unequal
// rather than equal-by-rounding.
const maxExactInt = int64(1) << 53

// asNumber reads v's numeric value, for any type whose kind is numeric —
// including a named type over one. A bool is not a number.
func asNumber(v any) (number, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return number{i: rv.Int(), kind: numInt}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return number{u: rv.Uint(), kind: numUint}, true
	case reflect.Float32, reflect.Float64:
		return number{f: rv.Float(), kind: numFloat}, true
	default:
		return number{}, false
	}
}

func equalNumbers(a, b number) bool {
	switch {
	case a.kind == numFloat && b.kind == numFloat:
		return a.f == b.f
	case a.kind == numFloat:
		return equalNumbers(b, a)
	case b.kind == numFloat:
		if a.kind == numInt {
			return exactIntFloat(a.i, b.f)
		}
		if a.u > uint64(maxExactInt) {
			return false
		}
		return exactIntFloat(int64(a.u), b.f)
	case a.kind == numInt && b.kind == numInt:
		return a.i == b.i
	case a.kind == numUint && b.kind == numUint:
		return a.u == b.u
	case a.kind == numInt:
		return a.i >= 0 && uint64(a.i) == b.u
	default:
		return b.i >= 0 && uint64(b.i) == a.u
	}
}

// exactIntFloat reports whether i and f are the same number, for an i float64
// spells exactly. Beyond that range the answer is no: the two cannot be told
// apart, and this comparison exists to catch a difference.
func exactIntFloat(i int64, f float64) bool {
	if i > maxExactInt || i < -maxExactInt {
		return false
	}
	return float64(i) == f
}
