// Package main: feature demo — kongfig/validation package.
// Shows per-key validators, cross-key rules (Rule[T]),
// schema annotation validators (Schema[T] with "required"), and WithNotifyOnLoad.
//
// Run:
//
//	go run ./example/features/validation
//	go run ./example/features/validation          # all valid → no errors
//	go run ./example/features/validation --bad    # load invalid data → errors
package main

import (
	"context"
	"fmt"
	"os"

	kongfig "github.com/pmarschik/kongfig"
	structsprovider "github.com/pmarschik/kongfig/providers/structs"
	"github.com/pmarschik/kongfig/validation"
)

// --- Feature: struct types used by cross-key and schema validators ---

type DBConfig struct {
	Host     string `kongfig:"host,required"`
	Port     int    `kongfig:"port"`
	MinConns int    `kongfig:"min_conns"`
	MaxConns int    `kongfig:"max_conns"`
}

type AppConfig struct {
	DB   DBConfig `kongfig:"db"`
	Port int      `kongfig:"port,required"`
}

// ---

var validDefaults = AppConfig{
	Port: 8080,
	DB:   DBConfig{Host: "localhost", Port: 5432, MinConns: 2, MaxConns: 10},
}

var invalidDefaults = AppConfig{
	Port: -1,
	DB:   DBConfig{Host: "", Port: 5432, MinConns: 10, MaxConns: 2},
}

// --- Feature: per-key validators as named functions ---

func validatePortRange(e validation.Event) []validation.FieldViolation {
	n, ok := e.Value.(int)
	if !ok || (n >= 1 && n <= 65535) {
		return nil
	}
	return []validation.FieldViolation{{
		Message:  fmt.Sprintf("port %d is out of range 1–65535", n),
		Code:     "port.range",
		Severity: validation.SeverityError,
	}}
}

// validatePortPositive is a warning-level per-load validator (does not reject Load).
func validatePortPositive(e validation.Event) []validation.FieldViolation {
	n, ok := e.Value.(int)
	if !ok || n > 0 {
		return nil
	}
	return []validation.FieldViolation{{
		Message:  "port must be positive (per-load advisory)",
		Code:     "port.positive",
		Severity: validation.SeverityWarning,
	}}
}

// dbConnRule is a flat projection struct for the db connection rule.
// Tags are full dot-paths from the config root — no At() prefix needed.
type dbConnRule struct {
	MinConns int `kongfig:"db.min_conns"`
	MaxConns int `kongfig:"db.max_conns"`
}

// validateDBConns is used as the rule function for Rule[dbConnRule].
func validateDBConns(db dbConnRule) []validation.FieldViolation {
	if db.MaxConns < db.MinConns {
		return []validation.FieldViolation{{
			Message:  fmt.Sprintf("db.max_conns (%d) must be >= db.min_conns (%d)", db.MaxConns, db.MinConns),
			Code:     "db.conns.order",
			Severity: validation.SeverityError,
		}}
	}
	return nil
}

// ---

func pathStrings(paths []validation.PathSource) []string {
	out := make([]string, len(paths))
	for i, ps := range paths {
		out[i] = ps.Path
	}
	return out
}

func printDiagnostics(d *validation.Diagnostics) {
	if len(d.LoadViolations) > 0 {
		fmt.Println("\nPer-load violations (accumulated during Load calls):")
		for i := range d.LoadViolations {
			lv := &d.LoadViolations[i]
			for _, v := range lv.Violations {
				fmt.Printf("  [%s] %s: %s\n", v.Severity, pathStrings(v.Paths), v.Message)
			}
		}
	}
	if len(d.Violations) > 0 {
		fmt.Println("\nFinal-state violations:")
		for _, v := range d.Violations {
			fmt.Printf("  [%s] %s: %s\n", v.Severity, pathStrings(v.Paths), v.Message)
		}
	}
}

func main() {
	bad := len(os.Args) > 1 && os.Args[1] == "--bad"

	ctx := context.Background()
	kf := kongfig.New()

	// --- Feature: configure all three validator types ---
	// WithNotifyOnLoad fires all validators on each Load; violations accumulate
	// in LoadViolations and never reject a Load — surface via Validate() at the end.
	v := validation.NewWithDefaults(validation.WithNotifyOnLoad())

	// Per-key: fires on Load (via WithNotifyOnLoad) and during Validate()
	v.AddValidator("port", validatePortRange)

	// --- Feature: cross-key rule (Rule[T]) ---
	// Projection struct tags are full dot-paths from root; no At() prefix needed.
	v.AddRule(validation.Rule(validateDBConns))

	// --- Feature: schema annotation validator (Schema[T] with built-in "required") ---
	v.AddSchema(validation.Schema[AppConfig]())

	// --- Feature: per-load warning accumulation ---
	v.AddValidator("port", validatePortPositive)
	v.Register(kf)
	// ---

	if bad {
		fmt.Println("Loading invalid config (port=-1, db.host empty, max_conns < min_conns)…")
		kf.MustLoad(ctx, structsprovider.Defaults(invalidDefaults))
	} else {
		fmt.Println("Loading valid config…")
		kf.MustLoad(ctx, structsprovider.Defaults(validDefaults))
	}

	// --- Feature: Validate — runs all validators on the final merged state ---
	d := v.Validate(kf)
	// ---

	if d == nil {
		fmt.Println("Config is valid.")
		return
	}
	printDiagnostics(d)
	if err := d.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "\nConfig invalid:", err)
		os.Exit(1)
	}
}
