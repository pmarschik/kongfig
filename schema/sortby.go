package schema

import "reflect"

// KeySortByPaths reflects on T and returns a map of map-path → sort spec, from
// the sortby= options on its struct tags. The spec is a value name, optionally
// dotted to reach one level down, and optionally prefixed with "-" for
// descending order:
//
//	type Config struct {
//	    Rules map[string]Rule `kongfig:"rules,sortby=-priority"`
//	}
//
// The mark names the map whose entries are put in order, so the path is the
// map's own path — "rules" above, and "profiles.*.rules" for a map inside a map
// value type. A mark on anything but a map is left out: a struct's children are
// distinct fields, ordered by declaration, so a value inside them says nothing
// about their order.
//
// Renderers consume this through [kongfig.KeySortByKey], which [kongfig.NewFor]
// populates. Returns nil when nothing is marked.
func KeySortByPaths[T any]() map[string]string {
	out := make(map[string]string)
	walkSortByFields(reflect.TypeFor[T](), "", out, map[reflect.Type]bool{})
	if len(out) == 0 {
		return nil
	}
	return out
}

// walkSortByFields collects the marks on typ's fields and descends into the
// types they hold. active holds the types on the current path, so a recursive
// type is entered once rather than forever.
func walkSortByFields(typ reflect.Type, prefix string, out map[string]string, active map[reflect.Type]bool) {
	typ = derefType(typ)
	if typ.Kind() != reflect.Struct || active[typ] {
		return
	}
	active[typ] = true
	defer delete(active, typ)

	for field := range typ.Fields() {
		if field.Anonymous {
			walkSortByFields(field.Type, prefix, out, active)
			continue
		}
		if !field.IsExported() {
			continue
		}
		ft := ParseFieldTag(field.Tag.Get("kongfig"), field.Name)
		if ft.Skip {
			continue
		}
		path := ft.Name
		if prefix != "" {
			path = prefix + "." + ft.Name
		}
		fieldTyp := derefType(field.Type)
		if ft.SortBy != "" && fieldTyp.Kind() == reflect.Map {
			out[path] = ft.SortBy
		}
		_, elemTyp, elemPrefix := inlineTargets(fieldTyp, path)
		walkSortByFields(elemTyp, elemPrefix, out, active)
	}
}
