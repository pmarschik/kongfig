package kongfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// ── Migration event types ──────────────────────────────────────────────────────

// MigrationEvent is a sealed sum type representing a migration condition detected
// during config loading. Handle with a type switch:
//
//	switch ev := e.(type) {
//	case kongfig.RenameEvent:    // old key moved to new key
//	case kongfig.ConflictEvent:  // both old and new keys present; old dropped
//	case kongfig.LegacyFileEvent: // deprecated config file discovered
//	}
type MigrationEvent interface{ migrationEvent() }

// RenameEvent fires when oldPath is present in a freshly loaded layer and newPath
// is absent. The value has already been moved to newPath before the handler runs.
type RenameEvent struct {
	OldPath    string // deprecated key path
	NewPath    string // replacement key path
	Value      any    // value that was moved
	SourceName string // layer source label, e.g. "xdg.yaml"
	SourceFile string // absolute file path; "" if source is not a file
	Occurrence int    // 1 = first time seen this process run; 2+ = repeat
}

// ConflictEvent fires when both oldPath and newPath are present in the same layer.
// OldPath has been dropped; NewPath value is preserved.
type ConflictEvent struct {
	OldPath    string
	NewPath    string
	OldValue   any
	NewValue   any
	SourceName string
	SourceFile string
	Occurrence int
}

// LegacyFileEvent fires when a deprecated config file is found by a wrapped discoverer.
type LegacyFileEvent struct {
	FilePath      string // absolute path to the deprecated file
	PreferredPath string // migration hint passed to discover.Deprecated
	SourceName    string // source label for the layer
	Occurrence    int
}

func (RenameEvent) migrationEvent()     {}
func (ConflictEvent) migrationEvent()   {}
func (LegacyFileEvent) migrationEvent() {}

// ── Handler and built-ins ──────────────────────────────────────────────────────

// MigrationResult is returned by MigrationFunc.
// A non-nil Err causes Load to fail; a non-empty Warning is non-fatal and
// accumulated in the Kongfig instance — retrieve with [Kongfig.MigrationWarnings].
// If both are set the error takes precedence and the Warning is discarded.
type MigrationResult struct {
	Err     error
	Warning string
}

// MigrationFunc is called when a migration condition fires during Load.
// Return a non-nil Err to cause Load to fail; set Warning for a non-fatal diagnostic.
type MigrationFunc func(MigrationEvent) MigrationResult

// Built-in MigrationFunc values for common behaviors.
var (
	// MigrationSilent does nothing — suppress without logging.
	MigrationSilent MigrationFunc = func(MigrationEvent) MigrationResult { return MigrationResult{} }

	// MigrationDebug logs at slog.LevelDebug via slog.Default().
	MigrationDebug MigrationFunc = func(e MigrationEvent) MigrationResult {
		logMigration(slog.Default(), slog.LevelDebug, e)
		return MigrationResult{}
	}

	// MigrationInfo logs at slog.LevelInfo via slog.Default().
	MigrationInfo MigrationFunc = func(e MigrationEvent) MigrationResult {
		logMigration(slog.Default(), slog.LevelInfo, e)
		return MigrationResult{}
	}

	// MigrationWarn logs at slog.LevelWarn via slog.Default().
	MigrationWarn MigrationFunc = func(e MigrationEvent) MigrationResult {
		logMigration(slog.Default(), slog.LevelWarn, e)
		return MigrationResult{}
	}

	// MigrationFail returns an error, causing Load to fail.
	// Use to enforce migration at startup.
	MigrationFail MigrationFunc = func(e MigrationEvent) MigrationResult {
		return MigrationResult{Err: migrationError(e)}
	}

	// MigrationWarnResult returns a non-fatal warning that is accumulated in the
	// Kongfig instance without logging or failing the load.
	// Use [Kongfig.MigrationWarnings] to retrieve them after loading.
	//
	// Note: when used with [discover.Deprecated], warnings are silently dropped
	// because discoverers run before the Kongfig instance is available.
	MigrationWarnResult MigrationFunc = func(e MigrationEvent) MigrationResult {
		return MigrationResult{Warning: migrationMessage(e)}
	}
)

func logMigration(logger *slog.Logger, level slog.Level, e MigrationEvent) {
	ctx := context.Background()
	switch ev := e.(type) {
	case RenameEvent:
		logger.Log(ctx, level, "deprecated config key",
			"old", ev.OldPath, "new", ev.NewPath, "source", ev.SourceName)
	case ConflictEvent:
		logger.Log(ctx, level, "config key conflict: both old and new present; old dropped",
			"old", ev.OldPath, "new", ev.NewPath, "source", ev.SourceName)
	case LegacyFileEvent:
		logger.Log(ctx, level, "deprecated config file found",
			"path", ev.FilePath, "preferred", ev.PreferredPath, "source", ev.SourceName)
	}
}

func migrationMessage(e MigrationEvent) string {
	switch ev := e.(type) {
	case RenameEvent:
		return fmt.Sprintf("deprecated config key %q: rename to %q (source: %s)", ev.OldPath, ev.NewPath, ev.SourceName)
	case ConflictEvent:
		return fmt.Sprintf("config key conflict: both %q and %q present in %s; remove the old key", ev.OldPath, ev.NewPath, ev.SourceName)
	case LegacyFileEvent:
		return fmt.Sprintf("deprecated config file %q found; migrate to: %s", ev.FilePath, ev.PreferredPath)
	}
	return fmt.Sprintf("migration: unexpected event type %T", e)
}

func migrationError(e MigrationEvent) error {
	return errors.New(migrationMessage(e))
}

// ── Policy ─────────────────────────────────────────────────────────────────────

// MigrationPolicy configures how migration events are handled.
type MigrationPolicy struct {
	OnFirst  MigrationFunc // first occurrence this process run (default: MigrationWarn)
	OnRepeat MigrationFunc // subsequent occurrences (default: MigrationDebug)
}

// DefaultMigrationPolicy is used when AddRename or discover.Deprecated is called
// without an explicit policy.
var DefaultMigrationPolicy = MigrationPolicy{
	OnFirst:  MigrationWarn,
	OnRepeat: MigrationDebug,
}

// ── Internal rename state ──────────────────────────────────────────────────────

type renameEntry struct {
	policy MigrationPolicy
	old    string
	new    string
	seen   int
	mu     sync.Mutex
}

// dispatch increments the occurrence counter and fires the appropriate handler.
func (r *renameEntry) dispatch(e MigrationEvent) MigrationResult {
	r.mu.Lock()
	r.seen++
	occ := r.seen
	r.mu.Unlock()

	switch ev := e.(type) {
	case RenameEvent:
		ev.Occurrence = occ
		e = ev
	case ConflictEvent:
		ev.Occurrence = occ
		e = ev
	}

	h := r.policy.OnRepeat
	if occ == 1 {
		h = r.policy.OnFirst
	}
	if h == nil {
		return MigrationResult{}
	}
	return h(e)
}

// ── Warning accessors ──────────────────────────────────────────────────────────

// MigrationWarnings returns all non-fatal migration warnings accumulated across
// Load calls since creation or the last [ClearMigrationWarnings] call.
// Warnings are only added when a load completes successfully.
func (k *Kongfig) MigrationWarnings() []string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if len(k.cfg.migrationWarnings) == 0 {
		return nil
	}
	out := make([]string, len(k.cfg.migrationWarnings))
	copy(out, k.cfg.migrationWarnings)
	return out
}

// ClearMigrationWarnings discards all accumulated migration warnings.
func (k *Kongfig) ClearMigrationWarnings() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cfg.migrationWarnings = nil
}

// ── Public API ─────────────────────────────────────────────────────────────────

// AddRename registers a path rename migration.
//
// When oldPath is present in a freshly loaded layer:
//   - newPath absent: value moves to newPath; policy.OnFirst/OnRepeat fires with a [RenameEvent]
//   - newPath also present: old key dropped; policy.OnFirst/OnRepeat fires with a [ConflictEvent]
//
// Supports dot-delimited leaf paths ("db.host") and whole subtree renames ("database" → "db").
// policy is optional; [DefaultMigrationPolicy] is used when omitted.
func (k *Kongfig) AddRename(oldPath, newPath string, policy ...MigrationPolicy) {
	p := DefaultMigrationPolicy
	if len(policy) > 0 {
		p = policy[0]
	}
	k.mu.Lock()
	k.cfg.renames = append(k.cfg.renames, &renameEntry{old: oldPath, new: newPath, policy: p})
	k.mu.Unlock()
}

// ── Internal apply ─────────────────────────────────────────────────────────────

// applyRenames applies all registered renames to incoming layer data.
// All renames are attempted; errors are collected and joined.
// Warnings (non-fatal results) are returned separately for the caller to accumulate.
func (k *Kongfig) applyRenames(data ConfigData, sourceName, sourceFile string) (ConfigData, []string, error) {
	k.mu.RLock()
	renames := make([]*renameEntry, len(k.cfg.renames))
	copy(renames, k.cfg.renames)
	k.mu.RUnlock()

	if len(renames) == 0 {
		return data, nil, nil
	}

	var errs []error
	var warnings []string
	for _, r := range renames {
		oldVal, oldExists := data.LookupPath(r.old)
		if !oldExists {
			continue
		}
		newVal, newExists := data.LookupPath(r.new)
		data = deleteNestedPath(data, r.old)

		var event MigrationEvent
		if newExists {
			event = ConflictEvent{
				OldPath: r.old, NewPath: r.new,
				OldValue: oldVal, NewValue: newVal,
				SourceName: sourceName, SourceFile: sourceFile,
			}
		} else {
			data = setNestedPath(data, r.new, oldVal)
			event = RenameEvent{
				OldPath: r.old, NewPath: r.new, Value: oldVal,
				SourceName: sourceName, SourceFile: sourceFile,
			}
		}

		res := r.dispatch(event)
		if res.Err != nil {
			errs = append(errs, res.Err)
		} else if res.Warning != "" {
			warnings = append(warnings, res.Warning)
		}
	}

	return data, warnings, errors.Join(errs...)
}

// deleteNestedPath removes the value at the dot-delimited path from a clone of d.
func deleteNestedPath(d ConfigData, path string) ConfigData {
	d = d.Clone()
	deleteNested(d, strings.Split(path, "."))
	return d
}

func deleteNested(m ConfigData, parts []string) {
	if len(parts) == 1 {
		delete(m, parts[0])
		return
	}
	sub, ok := m[parts[0]].(ConfigData)
	if !ok {
		return
	}
	deleteNested(sub, parts[1:])
}

// setNestedPath sets the value at the dot-delimited path in a clone of d.
func setNestedPath(d ConfigData, path string, val any) ConfigData {
	d = d.Clone()
	setNested(d, strings.Split(path, "."), val)
	return d
}

func setNested(m ConfigData, parts []string, val any) {
	if len(parts) == 1 {
		m[parts[0]] = val
		return
	}
	sub, ok := m[parts[0]].(ConfigData)
	if !ok {
		sub = make(ConfigData)
		m[parts[0]] = sub
	}
	setNested(sub, parts[1:], val)
}
