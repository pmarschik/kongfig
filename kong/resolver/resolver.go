// Package resolver provides a [kong.Resolver] backed by a [kongfig.Kongfig] instance.
// It seeds kong flag defaults from the merged config layers so that
// --help shows the effective value and explicit CLI flags still win.
package resolver

import (
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
	kongfig "github.com/pmarschik/kongfig"
)

type kongfigResolver struct {
	k *kongfig.Kongfig
}

// New returns a kong.Resolver that reads flag defaults from k.
// The resolver uses the flag's config:"" tag as the key path, falling back
// to the flag name with hyphens replaced by dots.
//
// If k has a [kongfig.ConfigValidator] registered via [kongfig.WithValidator],
// it is called automatically after kong parses flags — no extra wiring needed.
func New(k *kongfig.Kongfig) kong.Resolver {
	return &kongfigResolver{k: k}
}

func (r *kongfigResolver) Validate(_ *kong.Application) error {
	return r.k.Validate()
}

func (r *kongfigResolver) Resolve(_ *kong.Context, _ *kong.Path, flag *kong.Flag) (any, error) {
	path := flagPath(flag)
	val := walkPath(r.k.All(), strings.Split(path, "."))
	if val == nil {
		return nil, nil
	}
	return fmt.Sprintf("%v", val), nil
}

// flagPath mirrors the logic in kong/provider — config tag wins, then hyphen→dot.
func flagPath(flag *kong.Flag) string {
	if cfg := flag.Tag.Get("config"); cfg != "" && cfg != "-" {
		return cfg
	}
	return strings.ReplaceAll(flag.Name, "-", ".")
}

// walkPath traverses a nested map along dot-path parts.
// Returns nil if any segment is missing.
func walkPath(m kongfig.ConfigData, parts []string) any {
	if len(parts) == 0 || m == nil {
		return nil
	}
	val, ok := m[parts[0]]
	if !ok {
		return nil
	}
	if len(parts) == 1 {
		return val
	}
	sub, ok := val.(kongfig.ConfigData)
	if !ok {
		return nil
	}
	return walkPath(sub, parts[1:])
}
