// Package structs provides [kongfig.Provider] implementations that extract
// configuration from Go struct field tags.
package structs

import (
	"context"
	"maps"
	"os"
	"reflect"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	envprovider "github.com/pmarschik/kongfig/providers/env"
	"github.com/pmarschik/kongfig/schema"
)

// staticProvider holds a pre-built map and source label.
type staticProvider struct {
	providerData kongfig.ProviderData
	data         kongfig.ConfigData
	source       string
	kind         string
}

func (p *staticProvider) Load(_ context.Context) (kongfig.ConfigData, error) { return p.data, nil }
func (p *staticProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: p.source, Kind: p.kind}
}
func (p *staticProvider) ProviderData() kongfig.ProviderData { return p.providerData }

// Defaults reflects on v (must be a struct or pointer to struct) and returns
// a Provider that yields the field values keyed by their kongfig tag paths.
// Zero-value fields are omitted. Source label is "defaults".
func Defaults(v any) kongfig.Provider {
	data := extractValues(reflect.ValueOf(v), reflect.TypeOf(v))
	return &staticProvider{data: data, source: "defaults", kind: kongfig.KindDefaults}
}

// TagDefaults returns a Provider seeded from the default= tag annotations on T.
// Fields annotated with default=value in their kongfig struct tag are included
// using that value as the default. Fields without a default= annotation are omitted.
// Source label is "defaults".
//
//	type Config struct {
//	    Host string `kongfig:"host,default=localhost"`
//	    Port int    `kongfig:"port,default=8080"`
//	    Name string `kongfig:"name"` // omitted: no default= annotation
//	}
//	p := structsprovider.TagDefaults[Config]() // yields {host: "localhost", port: "8080"}
func TagDefaults[T any]() kongfig.Provider {
	data := extractTagDefaults(reflect.TypeFor[T]())
	return &staticProvider{data: data, source: "defaults", kind: kongfig.KindDefaults}
}

// extractTagDefaults walks the struct type and builds a nested map from default= tag annotations.
func extractTagDefaults(typ reflect.Type) kongfig.ConfigData {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return kongfig.ConfigData{}
	}

	out := make(kongfig.ConfigData)
	for field := range typ.Fields() {
		// Anonymous (embedded) struct: squash fields into current map.
		if field.Anonymous {
			maps.Copy(out, extractTagDefaults(field.Type))
			continue
		}

		if !field.IsExported() {
			continue
		}

		ft := schema.ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if ft.Skip {
			continue
		}

		// Nested struct: recurse.
		ftype := field.Type
		for ftype.Kind() == reflect.Pointer {
			ftype = ftype.Elem()
		}
		if ftype.Kind() == reflect.Struct {
			sub := extractTagDefaults(ftype)
			if len(sub) > 0 {
				out[ft.Name] = sub
			}
			continue
		}

		// Leaf: include only if a default= annotation is present.
		if ft.Default != nil {
			out[ft.Name] = *ft.Default
		}
	}
	return out
}

// TagEnv returns a Provider that reads env vars declared via env:"" struct tags on T.
// Only env vars present in os.Environ() are included. Source label is "env.tag".
func TagEnv[T any]() kongfig.Provider {
	var zero T
	data := extractEnv(reflect.TypeOf(zero))
	return &staticProvider{data: data, source: "env.tag", kind: kongfig.KindEnv, providerData: envprovider.ProviderData{}}
}

// derefType dereferences pointer types to their underlying type.
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// derefValue dereferences a pointer value; returns the zero Value if nil.
func derefValue(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	return v, true
}

// extractValues recursively walks the struct and builds a nested map from field values.
func extractValues(val reflect.Value, typ reflect.Type) kongfig.ConfigData {
	val, ok := derefValue(val)
	if !ok {
		return kongfig.ConfigData{}
	}
	typ = derefType(typ)
	if val.Kind() != reflect.Struct {
		return kongfig.ConfigData{}
	}

	out := make(kongfig.ConfigData)
	for i := range typ.NumField() {
		field := typ.Field(i)
		fval := val.Field(i)

		// Anonymous (embedded) struct: squash fields into current map.
		// Handle before the exported check — the embedded type itself may be
		// unexported (lowercase name) but its fields may be exported.
		if field.Anonymous {
			maps.Copy(out, extractValues(fval, field.Type))
			continue
		}

		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get("kongfig")
		if tag == "-" {
			continue
		}

		name := parseTag(tag, field.Name)

		// Nested struct: recurse.
		if derefType(field.Type).Kind() == reflect.Struct {
			subVal, ok := derefValue(fval)
			if !ok {
				continue
			}
			sub := extractValues(subVal, derefType(field.Type))
			if len(sub) > 0 {
				out[name] = sub
			}
			continue
		}

		// Leaf value: skip zero values.
		if fval.IsZero() {
			continue
		}
		out[name] = fval.Interface()
	}
	return out
}

// extractEnv walks the struct type and reads env var values from the environment.
func extractEnv(typ reflect.Type) kongfig.ConfigData {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return kongfig.ConfigData{}
	}

	out := make(kongfig.ConfigData)
	for field := range typ.Fields() {
		// Anonymous embedded: squash before exported check (type may be unexported).
		if field.Anonymous {
			maps.Copy(out, extractEnv(field.Type))
			continue
		}

		if !field.IsExported() {
			continue
		}

		kongfigTag := field.Tag.Get("kongfig")
		if kongfigTag == "-" {
			continue
		}
		name := parseTag(kongfigTag, field.Name)

		// Nested struct: recurse.
		ftype := field.Type
		for ftype.Kind() == reflect.Pointer {
			ftype = ftype.Elem()
		}
		if ftype.Kind() == reflect.Struct {
			sub := extractEnv(ftype)
			if len(sub) > 0 {
				out[name] = sub
			}
			continue
		}

		// Leaf: look up env tag.
		envName := field.Tag.Get("env")
		if envName == "" {
			continue
		}
		val, ok := os.LookupEnv(envName)
		if !ok {
			continue
		}
		out[name] = val
	}
	return out
}

// parseTag returns the key name from a kongfig struct tag.
// Falls back to lowercased field name if tag is empty.
func parseTag(tag, fieldName string) string {
	if tag == "" {
		return strings.ToLower(fieldName)
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return strings.ToLower(fieldName)
	}
	return name
}
