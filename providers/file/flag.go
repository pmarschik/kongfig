package file

import (
	"context"
	"fmt"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/schema"
)

// configPathsToSlice normalises a config path value to a slice of strings.
// The value may be a string (single path) or a []string / []any produced by sep= splitting.
// Empty strings are excluded so callers do not need to filter them separately.
func configPathsToSlice(v any) []string {
	switch v := v.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []string:
		return v
	case []any:
		paths := make([]string, 0, len(v))
		for _, elem := range v {
			if s, ok := elem.(string); ok {
				paths = append(paths, s)
			}
		}
		return paths
	default:
		return nil
	}
}

// LoadConfigPaths loads files for each entry in entries by reading the path value
// from kf's current merged state (via the entry's Key) and loading the file.
// Entries are processed in the order given; use [kongfig.Kongfig.ConfigPaths] or
// [kongfig.ConfigPaths] to obtain a pre-sorted slice.
//
// All path values are resolved from a single snapshot of kf taken at call time,
// before any file is loaded. A file loaded for one entry cannot influence the path
// resolution for later entries.
//
// Returns on the first error. Files whose key is absent or empty are silently skipped.
func LoadConfigPaths(ctx context.Context, kf *kongfig.Kongfig, entries []schema.ConfigPathEntry, opts ...kongfig.LoadOption) error {
	return kf.DeriveLoad(ctx, func(in kongfig.DeriveInput) ([]kongfig.Provider, error) {
		flat := in.Data.FlatValues()
		var providers []kongfig.Provider
		for _, e := range entries {
			pathVal, ok := flat[e.Key]
			if !ok {
				continue
			}
			for _, path := range configPathsToSlice(pathVal) {
				if path == "" {
					continue
				}
				parser, err := kongfig.ParserForPath(path, kf.Parsers())
				if err != nil {
					return nil, fmt.Errorf("file: config key %q = %q: %w", e.Key, path, err)
				}
				providers = append(providers, New(path, parser))
			}
		}
		return providers, nil
	}, opts...)
}

// MustLoadConfigPaths is like [LoadConfigPaths] but panics on error.
func MustLoadConfigPaths(ctx context.Context, kf *kongfig.Kongfig, entries []schema.ConfigPathEntry, opts ...kongfig.LoadOption) {
	if err := LoadConfigPaths(ctx, kf, entries, opts...); err != nil {
		panic(err)
	}
}
