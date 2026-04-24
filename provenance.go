package kongfig

import (
	"maps"
	"sort"
)

// Provenance records the config source for each key path.
type Provenance struct {
	sources map[string]SourceMeta // path -> source attribution
}

// NewProvenance returns an empty Provenance.
func NewProvenance() *Provenance {
	return &Provenance{
		sources: make(map[string]SourceMeta),
	}
}

// Set records that key path was set by the given source.
func (p *Provenance) Set(path string, sm SourceMeta) {
	p.sources[path] = sm
}

// SourceMetas returns a snapshot of path→SourceMeta mappings.
func (p *Provenance) SourceMetas() map[string]SourceMeta {
	out := make(map[string]SourceMeta, len(p.sources))
	maps.Copy(out, p.sources)
	return out
}

// clone returns a deep copy of p.
func (p *Provenance) clone() *Provenance {
	out := NewProvenance()
	maps.Copy(out.sources, p.sources)
	return out
}

// Paths returns all tracked paths in sorted order.
func (p *Provenance) Paths() []string {
	paths := make([]string, 0, len(p.sources))
	for k := range p.sources {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	return paths
}
