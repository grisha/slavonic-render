package expreval

import (
	"errors"
	"fmt"
	"testing"
)

func TestEval(t *testing.T) {
	// No variables: a nil map says nothing of their type, so it has one.
	var none map[string]string

	t.Run("simple string", func(t *testing.T) {
		got, err := Eval(`"hello"`, none, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})

	t.Run("arithmetic", func(t *testing.T) {
		got, err := Eval(`1 + 2`, none, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != "3" {
			t.Errorf("got %q, want %q", got, "3")
		}
	})

	t.Run("custom function", func(t *testing.T) {
		funcs := map[string]any{
			"greet": func(name string) string { return "hi " + name },
		}
		got, err := Eval(`greet("world")`, none, funcs)
		if err != nil {
			t.Fatal(err)
		}
		if got != "hi world" {
			t.Errorf("got %q, want %q", got, "hi world")
		}
	})

	t.Run("builtin range", func(t *testing.T) {
		got, err := Eval(`range(x)`, map[string][]string{"x": {"a", "b"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != "a\u2013b" {
			t.Errorf("got %q, want %q", got, "a\u2013b")
		}
	})

	t.Run("undefined variable error", func(t *testing.T) {
		_, err := Eval(`undefined_var`, none, nil)
		if err == nil {
			t.Fatal("expected error for undefined variable")
		}
	})

	t.Run("ErrNeedsDefer propagation", func(t *testing.T) {
		funcs := map[string]any{
			"deferred": func() (string, error) {
				return "", fmt.Errorf("not ready: %w", ErrNeedsDefer)
			},
		}
		_, err := Eval(`deferred()`, none, funcs)
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrNeedsDefer) {
			t.Errorf("expected ErrNeedsDefer, got: %v", err)
		}
	})

	t.Run("vars resolve in expression", func(t *testing.T) {
		vars := map[string][]string{"greeting": {"HELLO"}}
		got, err := Eval(`greeting`, vars, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != "HELLO" {
			t.Errorf("got %q, want %q", got, "HELLO")
		}
	})

	t.Run("vars array indexing", func(t *testing.T) {
		vars := map[string][]string{"items": {"a", "b", "c"}}
		got, err := Eval(`items[1]`, vars, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != "b" {
			t.Errorf("got %q, want %q", got, "b")
		}
	})

	t.Run("env shadows vars", func(t *testing.T) {
		vars := map[string][]string{"range": {"shadowed"}, "x": {"a", "b"}}
		// range is a helper of the environment, which wins over a variable
		// of the same name.
		got, err := Eval(`range(x)`, vars, nil)
		if err != nil {
			t.Fatal(err)
		}
		if want := "a\u2013b"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// first and last are expr-lang's own, over a list: a key of the page
	// context.
	t.Run("first and last of expr-lang", func(t *testing.T) {
		vars := map[string][]string{"x": {"a", "b", "c"}, "empty": {}}
		for expr, want := range map[string]string{
			`first(x)`:        "a",
			`last(x)`:         "c",
			`first(empty)`:    "",
			`last(empty)`:     "",
			`upper(first(x))`: "A",
		} {
			got, err := Eval(expr, vars, nil)
			if err != nil {
				t.Errorf("%s: %v", expr, err)
				continue
			}
			if got != want {
				t.Errorf("%s: got %q, want %q", expr, got, want)
			}
		}
	})

	// A key of the front matter is a string, not a list of one: it goes to
	// a function, a comparison or a sum as it is, and a number is text.
	t.Run("string variables", func(t *testing.T) {
		vars := map[string]string{"Feast": "Pascha", "Tone": "5"}
		for expr, want := range map[string]string{
			`Feast`:                            "Pascha",
			`upper(Feast)`:                     "PASCHA",
			`Feast == "Pascha" ? "yes" : "no"`: "yes",
			`Feast + "!"`:                      "Pascha!",
			`int(Tone) + 1`:                    "6",
			`len(Feast)`:                       "6",
		} {
			got, err := Eval(expr, vars, nil)
			if err != nil {
				t.Errorf("%s: %v", expr, err)
				continue
			}
			if got != want {
				t.Errorf("%s: got %q, want %q", expr, got, want)
			}
		}
		// A string is not a list of one: what takes a list is an error.
		for _, expr := range []string{`first(Feast)`, `Feast[0]`} {
			if got, err := Eval(expr, vars, nil); err == nil {
				t.Errorf("%s: got %q, want an error", expr, got)
			}
		}
	})
}
