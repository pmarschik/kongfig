// Package expr implements the parser for kongfig validate= annotation expressions.
//
// # Expression Grammar
//
// An expression is one of:
//   - Atom: a bare name or numeric literal (e.g. "required", "hostname", "1")
//   - Call: name(args...) where args are space-separated expressions (e.g. "min(1)", "any(hostname ip)")
//
// Multiple top-level space-separated expressions are implicitly wrapped in all():
//
//	"min(1) max(100)"  →  all(min(1) max(100))
//	"required hostname"  →  all(required hostname)
//
// String arguments use single quotes:
//
//	pattern('[0-9]+')
//	oneof('debug' 'info' 'warn' 'error')
//
// Nested calls as arguments are not supported — "all(min(max(1)))" is a parse error.
// Use flat composition instead: "min(1) max(100)".
//
// # Built-in Combinators
//
//   - all(e1 e2 ...): passes if every sub-expression passes (conjunction)
//   - any(e1 e2 ...): passes if at least one sub-expression passes (disjunction)
//   - each(e): for slice/array values, applies e to every element
//   - keys(e): for map values, applies e to every map key
//
// The grammar is defined in grammar.peg and grammar_gen.go is generated from it.
package expr

import "fmt"

//go:generate go run github.com/mna/pigeon -o grammar_gen.go grammar.peg

// Expr is an AST node for a parsed validate= expression.
//
// Atoms (validators without arguments): Name is set, Args is nil.
//
//	Expr{Name: "required"}       // required
//	Expr{Name: "hostname"}       // hostname
//	Expr{Name: "1"}              // numeric literal, e.g. as an arg to min
//
// Calls (validators with explicit parentheses): Name is the function name, Args is non-nil.
// A zero-argument call has Args == []Expr{}.
//
//	Expr{Name: "min", Args: []Expr{{Name: "1"}}}                 // min(1)
//	Expr{Name: "any", Args: []Expr{{Name: "hostname"}, ...}}     // any(hostname ip)
type Expr struct {
	Name string
	Args []Expr // nil = atom; non-nil (may be empty) = call
}

// IsCall reports whether e is a function call (as opposed to an atom).
// A zero-argument call like foo() returns true; an atom like foo returns false.
func (e Expr) IsCall() bool { return e.Args != nil }

// ParseExpr parses a validate= expression string into an Expr AST.
// Returns a descriptive error if the input does not conform to the grammar.
func ParseExpr(s string) (Expr, error) {
	result, err := Parse("", []byte(s))
	if err != nil {
		return Expr{}, err
	}
	e, ok := result.(Expr)
	if !ok {
		return Expr{}, fmt.Errorf("kongfig/validation/expr: internal: grammar produced %T", result)
	}
	return e, nil
}
