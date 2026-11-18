// Package expreval provides expression evaluation for ${...} variable expansion.
// It uses expr-lang/expr to evaluate expressions against a variable environment,
// supporting ternary operators, function calls, and array operations.
package expreval

import (
	"errors"
	"fmt"
	"strings"

	"github.com/expr-lang/expr"
)

// ErrNeedsDefer should be wrapped by custom functions (like page()) to signal
// that the expression should be deferred rather than treated as a fatal error.
// Any other error is a mistake in the expression: Eval returns it, and Expand
// leaves the expression in the text as it is.
var ErrNeedsDefer = errors.New("needs deferral")

// Expand replaces ${...} references in s by evaluating each as an expr-lang
// expression against vars.
// A variable is a string (a key of the front matter) or a list of strings
// (a key of the page context), as its map has it.
// Expressions that fail to compile or evaluate, or that need deferral, are
// left as they are written, ${…} and all, so a caller finds them there.
func Expand[V string | []string](s string, vars map[string]V) string {
	if !strings.Contains(s, "${") {
		return s
	}
	env := buildEnv(vars)
	var b strings.Builder
	for {
		idx := strings.Index(s, "${")
		if idx < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:idx])
		s = s[idx+2:]
		// Find matching closing brace, respecting nested {}.
		depth := 1
		end := -1
		for i := 0; i < len(s); i++ {
			if s[i] == '{' {
				depth++
			}
			if s[i] == '}' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end < 0 {
			b.WriteString("${")
			continue
		}
		exprStr := s[:end]
		s = s[end+1:]

		resolved, err := evalWithEnv(exprStr, env)
		if err != nil {
			b.WriteString("${" + exprStr + "}")
			continue
		}
		b.WriteString(resolved)
	}
	return b.String()
}

// Eval evaluates a single bare expression (without ${...} delimiters)
// against the provided vars and env. vars are lowest priority, then
// builtin helpers, then env entries (highest). Returns the resolved string
// or an error wrapping ErrNeedsDefer if a custom function signals deferral.
// A variable is a string or a list of strings, as in Expand.
func Eval[V string | []string](exprStr string, vars map[string]V, env map[string]any) (string, error) {
	dftEnv := buildEnv(vars)
	for k, v := range env {
		dftEnv[k] = v
	}
	result, err := evalWithEnv(exprStr, dftEnv)
	if err != nil {
		return "", fmt.Errorf("Eval: %w", err)
	}
	return result, nil
}

// evalWithEnv compiles and runs a single expression against the given
// environment, returning the result as a string.
func evalWithEnv(exprStr string, env map[string]any) (string, error) {
	program, err := expr.Compile(exprStr, expr.Env(env))
	if err != nil {
		return "", err
	}
	result, err := expr.Run(program, env)
	if err != nil {
		return "", err
	}
	return resultToString(result), nil
}

// buildEnv converts the variable map to an expr environment and registers
// the helper range, which takes a list: a key of the page context, as
// len(), indexing and expr-lang's own first() and last() do. A key of the
// front matter is a string, for upper(), == and the rest.
func buildEnv[V string | []string](vars map[string]V) map[string]any {
	env := make(map[string]any, len(vars)+1)
	for k, v := range vars {
		env[k] = v
	}

	// range returns a smart range string: "value" for single, "first–last" for multiple.
	env["range"] = func(s []string) string {
		if len(s) == 0 {
			return ""
		}
		if len(s) == 1 {
			return s[0]
		}
		return s[0] + "\u2013" + s[len(s)-1]
	}
	return env
}

// resultToString converts an expr result to a string.
// []string returns the last element, so a single-element list returns its
// value. string is used as-is. Other types use fmt.Sprintf.
func resultToString(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case []string:
		if len(v) == 0 {
			return ""
		}
		return v[len(v)-1]
	case []any:
		if len(v) == 0 {
			return ""
		}
		return resultToString(v[len(v)-1])
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
