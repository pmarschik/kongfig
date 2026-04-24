package kongfig

import (
	"fmt"
	"strings"
)

// ConfigData is the canonical type for a configuration data map throughout kongfig.
// Keys are configuration field names; values may be nested ConfigData sub-trees or
// leaf values (string, int, bool, etc.).
//
// Use [ConfigData.LookupPath] to traverse dot-delimited paths without manual nesting.
type ConfigData map[string]any

// ToConfigData recursively converts a map[string]any (e.g. from a parser library)
// to ConfigData, replacing all nested map[string]any sub-trees with ConfigData.
// Also recurses into []any slices so maps nested inside slices are converted too.
func ToConfigData(m map[string]any) ConfigData {
	out := make(ConfigData, len(m))
	for k, v := range m {
		out[k] = normalizeAny(v)
	}
	return out
}

// normalizeAny recursively converts map[string]any to ConfigData and recurses into
// []any slices, so the entire value tree uses ConfigData for all map nodes.
func normalizeAny(v any) any {
	switch c := v.(type) {
	case ConfigData:
		return normalizeConfigData(c)
	case map[string]any:
		return ToConfigData(c)
	case []any:
		out := make([]any, len(c))
		for i, elem := range c {
			out[i] = normalizeAny(elem)
		}
		return out
	default:
		return v
	}
}

// LookupPath looks up a dot-delimited path in d, returning the value and whether it
// was found. An empty path returns d itself. Returns false if any segment is missing
// or if an intermediate value is not a ConfigData sub-tree.
func (d ConfigData) LookupPath(path string) (any, bool) {
	if path == "" {
		return d, true
	}
	return d.existsAt(strings.Split(path, "."))
}

// Clone returns a deep copy of d. Nested ConfigData sub-trees are cloned recursively.
// Mutations to the returned ConfigData do not affect d.
func (d ConfigData) Clone() ConfigData {
	out := make(ConfigData, len(d))
	for k, v := range d {
		if sub, ok := v.(ConfigData); ok {
			out[k] = sub.Clone()
		} else {
			out[k] = v
		}
	}
	return out
}

// SubTree returns a deep copy of the sub-tree rooted at the given dot-delimited path.
// Returns an empty ConfigData if the path does not exist or is not a ConfigData.
func (d ConfigData) SubTree(path string) ConfigData {
	sub := d.subTreeAt(strings.Split(path, "."))
	if sub == nil {
		return ConfigData{}
	}
	return sub.Clone()
}

// FlatValues returns a flat ConfigData mapping dot-delimited paths to their leaf values.
// Nested ConfigData sub-trees are recursed; values are returned without stringification.
func (d ConfigData) FlatValues() ConfigData {
	out := make(ConfigData)
	flatValuesInto(d, "", out)
	return out
}

// FlatStrings returns a flat map from dot-delimited path to string value.
// Each leaf value is formatted with fmt.Sprintf("%v", v).
func (d ConfigData) FlatStrings() map[string]string {
	out := make(map[string]string)
	flatStringsInto(d, "", out)
	return out
}

// existsAt reports whether the given path parts exist in d, returning the value if found.
func (d ConfigData) existsAt(parts []string) (any, bool) {
	if len(parts) == 0 {
		return d, true
	}
	v, ok := d[parts[0]]
	if !ok {
		return nil, false
	}
	if len(parts) == 1 {
		return v, true
	}
	sub, ok := v.(ConfigData)
	if !ok {
		return nil, false
	}
	return sub.existsAt(parts[1:])
}

// subTreeAt returns the sub-tree at the given path parts, or nil if not found.
// The returned ConfigData is NOT a clone; it aliases memory within d.
func (d ConfigData) subTreeAt(parts []string) ConfigData {
	if len(parts) == 0 {
		return d
	}
	v, ok := d[parts[0]]
	if !ok {
		return nil
	}
	sub, ok := v.(ConfigData)
	if !ok {
		return nil
	}
	if len(parts) == 1 {
		return sub
	}
	return sub.subTreeAt(parts[1:])
}

// mergeFrom deep-merges src into d, recording provenance and delta.
func (d ConfigData) mergeFrom(src ConfigData, sm SourceMeta, prov *Provenance, fns map[string]MergeFunc, delta ConfigData, prefix string) {
	for k, sv := range src {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if fn, ok := fns[path]; ok {
			if result, err := fn(d[k], sv); err == nil {
				d[k] = result
				prov.Set(path, sm)
				delta[path] = sv
				continue
			}
		}
		if srcSub, ok := sv.(ConfigData); ok {
			dstSub, ok := d[k].(ConfigData)
			if !ok {
				dstSub = make(ConfigData)
				d[k] = dstSub
			}
			dstSub.mergeFrom(srcSub, sm, prov, fns, delta, path)
		} else {
			old := d[k]
			d[k] = sv
			prov.Set(path, sm)
			if fmt.Sprintf("%v", old) != fmt.Sprintf("%v", sv) {
				delta[path] = sv
			}
		}
	}
}

// applyCodecs applies registered path codecs to leaf values, returning a new ConfigData
// with decoded (typed) values. Only paths present in codecs are affected; others pass through.
// Returns an error if any codec.decode call fails.
func (d ConfigData) applyCodecs(codecs map[string]anyCodec, prefix string) (ConfigData, error) {
	if len(codecs) == 0 {
		return d, nil
	}
	out := make(ConfigData, len(d))
	for key, val := range d {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if sub, ok := val.(ConfigData); ok {
			converted, err := sub.applyCodecs(codecs, path)
			if err != nil {
				return nil, err
			}
			out[key] = converted
			continue
		}
		if ac, ok := codecs[path]; ok {
			decoded, err := ac.decode(val)
			if err != nil {
				return nil, fmt.Errorf("kongfig: codec decode at %q: %w", path, err)
			}
			out[key] = decoded
		} else {
			out[key] = val
		}
	}
	return out, nil
}

// normalizeConfigData recursively converts any map[string]any sub-trees within d
// to ConfigData, so that all downstream .(ConfigData) assertions work correctly.
// Also normalizes []any slices so maps nested inside slices are converted too.
func normalizeConfigData(d ConfigData) ConfigData {
	out := make(ConfigData, len(d))
	for k, v := range d {
		out[k] = normalizeAny(v)
	}
	return out
}

// --- private helpers ---

func flatValuesInto(d ConfigData, prefix string, out ConfigData) {
	for k, v := range d {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(ConfigData); ok {
			flatValuesInto(sub, path, out)
		} else {
			out[path] = v
		}
	}
}

func flatStringsInto(d ConfigData, prefix string, out map[string]string) {
	for k, v := range d {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(ConfigData); ok {
			flatStringsInto(sub, path, out)
		} else {
			out[path] = fmt.Sprintf("%v", v)
		}
	}
}
