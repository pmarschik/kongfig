package kongfig

import (
	"maps"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/pmarschik/kongfig/schema"
)

// Internal get option keys.
var (
	getPathKey   = newGetOptionsKey[string]()
	getStrictKey = newGetOptionsKey[bool]()
)

// Get decodes the Kongfig into T using the kongfig struct tag for path mapping.
// Pass Strict() to fail on unknown keys, At("path") to decode a sub-tree.
func Get[T any](k *Kongfig, opts ...GetOption) (T, error) {
	var zero T
	cfg := applyGetOptions(opts)

	path, _ := readGet(cfg, getPathKey)
	strict, _ := readGet(cfg, getStrictKey)

	data := k.All()
	if path != "" {
		data = subTreeMap(data, path)
	}

	// Apply decode-only codecs (no Encode) here so raw values are preserved in the
	// store for rendering; typed values are visible only to Get consumers.
	var applyErr error
	data, applyErr = applyDecodeOnlyCodecs(k, data)
	if applyErr != nil {
		return zero, applyErr
	}

	// Reshape flat dot-keys (e.g. "ui.theme" from env providers) into nested maps
	// so path navigation works uniformly.
	data = reshapeKongfigTags(data)

	projection := buildPathsProjection(reflect.TypeFor[T](), data)

	// In strict mode, merge unknown top-level keys back so mapstructure can report them.
	if strict {
		for k, v := range data {
			if _, exists := projection[k]; !exists {
				projection[k] = v
			}
		}
	}

	var out T
	dcfg := &mapstructure.DecoderConfig{
		Result:  &out,
		TagName: "kongfig",
		// TODO: set WeaklyTypedInput: false once providers guarantee native types;
		// JSON-decoded numbers arrive as float64, which fails strict type matching.
		WeaklyTypedInput: true,
		Squash:           true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.TextUnmarshallerHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
		),
	}
	if strict {
		dcfg.ErrorUnused = true
	}

	dec, err := mapstructure.NewDecoder(dcfg)
	if err != nil {
		return zero, err
	}
	if err := dec.Decode(projection); err != nil {
		return zero, err
	}
	return out, nil
}

// GetWithProvenance decodes the Kongfig into T and returns it wrapped with provenance.
func GetWithProvenance[T any](k *Kongfig, opts ...GetOption) (WithProvenance[T], error) {
	val, err := Get[T](k, opts...)
	if err != nil {
		return WithProvenance[T]{}, err
	}
	return WithProvenance[T]{Value: val, Prov: k.Provenance()}, nil
}

func applyGetOptions(opts []GetOption) *getOptions {
	cfg := &getOptions{}
	for _, o := range opts {
		o(cfg)
	}
	return cfg
}

// GetOptionsPath returns the sub-tree path set by [At], or "" if no At option is present.
func GetOptionsPath(opts []GetOption) string {
	cfg := applyGetOptions(opts)
	path, _ := readGet(cfg, getPathKey)
	return path
}

// reshapeKongfigTags pre-processes the flat map: any key containing "." is
// expanded into a nested map (e.g. "ui.theme" → {"ui": {"theme": val}}).
// This normalises flat-key providers (env, flags) so path navigation works
// uniformly with nested-key providers (file parsers).
func reshapeKongfigTags(data ConfigData) ConfigData {
	out := data.Clone()
	for key, val := range data {
		if strings.Contains(key, ".") {
			parts := strings.Split(key, ".")
			delete(out, key)
			reshapeSetNested(out, parts, val)
		}
	}
	return out
}

func reshapeSetNested(m ConfigData, parts []string, val any) {
	if len(parts) == 1 {
		m[parts[0]] = val
		return
	}
	sub, ok := m[parts[0]].(ConfigData)
	if !ok {
		sub = make(ConfigData)
		m[parts[0]] = sub
	}
	reshapeSetNested(sub, parts[1:], val)
}

// subTreeMap extracts the nested map at the given dot-delimited path.
func subTreeMap(data ConfigData, path string) ConfigData {
	parts := strings.Split(path, ".")
	cur := data
	for _, p := range parts {
		v, ok := cur[p]
		if !ok {
			return ConfigData{}
		}
		sub, ok := v.(ConfigData)
		if !ok {
			return ConfigData{}
		}
		cur = sub
	}
	return cur
}

// buildPathsProjection builds a map keyed by each exported field's tag value,
// with values looked up in data. It handles:
//   - Nested structs with nested map values at the field path.
//   - Flat-key data (e.g. "ui.theme") from env/flag providers, by synthesizing a
//     sub-map for struct fields whose path is a prefix of keys in data.
//   - Dotted-path field tags (e.g. kongfig:"db.host") that navigate from root.
func buildPathsProjection(typ reflect.Type, data ConfigData) map[string]any {
	return buildProjectionWithPrefix(typ, data, "")
}

func buildProjectionWithPrefix(typ reflect.Type, data ConfigData, prefix string) map[string]any {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	flat := make(map[string]any)
	if typ.Kind() != reflect.Struct {
		return flat
	}
	for field := range typ.Fields() {
		if field.Anonymous {
			maps.Copy(flat, buildProjectionWithPrefix(field.Type, data, prefix))
			continue
		}
		if !field.IsExported() {
			continue
		}
		ft := schema.ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if ft.Skip {
			continue
		}
		fullPath := ft.Name
		if prefix != "" {
			fullPath = prefix + "." + ft.Name
		}
		if val, ok := navigatePath(data, fullPath); ok {
			flat[ft.Name] = val
			continue
		}
		// For struct fields not found directly, try building a sub-projection
		// from child paths (handles dotted-path tags on nested struct fields).
		ft2 := field.Type
		for ft2.Kind() == reflect.Pointer {
			ft2 = ft2.Elem()
		}
		if ft2.Kind() == reflect.Struct {
			sub := buildProjectionWithPrefix(field.Type, data, fullPath)
			if len(sub) > 0 {
				flat[ft.Name] = sub
			}
		}
	}
	return flat
}

// navigatePath looks up a value at the given dot-delimited path in m.
// Returns (value, true) if found, (nil, false) if any segment is missing.
func navigatePath(m ConfigData, path string) (any, bool) {
	var cur any = m
	for p := range strings.SplitSeq(path, ".") {
		cm, ok := cur.(ConfigData)
		if !ok {
			return nil, false
		}
		cur, ok = cm[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
