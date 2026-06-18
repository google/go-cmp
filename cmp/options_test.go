// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmp

import (
	"io"
	"reflect"
	"strings"
	"testing"

	ts "github.com/google/go-cmp/cmp/internal/teststructs"
)

// Test that the creation of Option values with non-sensible inputs produces
// a run-time panic with a decent error message
func TestOptionPanic(t *testing.T) {
	type myBool bool
	tests := []struct {
		label     string // Test description
		fnc       any    // Option function to call
		args      []any  // Arguments to pass in
		wantPanic string // Expected panic message
	}{{
		label: "AllowUnexported",
		fnc:   AllowUnexported,
		args:  []any{},
	}, {
		label:     "AllowUnexported",
		fnc:       AllowUnexported,
		args:      []any{1},
		wantPanic: "invalid struct type",
	}, {
		label: "AllowUnexported",
		fnc:   AllowUnexported,
		args:  []any{ts.StructA{}},
	}, {
		label: "AllowUnexported",
		fnc:   AllowUnexported,
		args:  []any{ts.StructA{}, ts.StructB{}, ts.StructA{}},
	}, {
		label:     "AllowUnexported",
		fnc:       AllowUnexported,
		args:      []any{ts.StructA{}, &ts.StructB{}, ts.StructA{}},
		wantPanic: "invalid struct type",
	}, {
		label:     "Comparer",
		fnc:       Comparer,
		args:      []any{5},
		wantPanic: "invalid comparer function",
	}, {
		label: "Comparer",
		fnc:   Comparer,
		args:  []any{func(x, y any) bool { return true }},
	}, {
		label: "Comparer",
		fnc:   Comparer,
		args:  []any{func(x, y io.Reader) bool { return true }},
	}, {
		label:     "Comparer",
		fnc:       Comparer,
		args:      []any{func(x, y io.Reader) myBool { return true }},
		wantPanic: "invalid comparer function",
	}, {
		label:     "Comparer",
		fnc:       Comparer,
		args:      []any{func(x string, y any) bool { return true }},
		wantPanic: "invalid comparer function",
	}, {
		label:     "Comparer",
		fnc:       Comparer,
		args:      []any{(func(int, int) bool)(nil)},
		wantPanic: "invalid comparer function",
	}, {
		label:     "Transformer",
		fnc:       Transformer,
		args:      []any{"", 0},
		wantPanic: "invalid transformer function",
	}, {
		label: "Transformer",
		fnc:   Transformer,
		args:  []any{"", func(int) int { return 0 }},
	}, {
		label: "Transformer",
		fnc:   Transformer,
		args:  []any{"", func(bool) bool { return true }},
	}, {
		label: "Transformer",
		fnc:   Transformer,
		args:  []any{"", func(int) bool { return true }},
	}, {
		label:     "Transformer",
		fnc:       Transformer,
		args:      []any{"", func(int, int) bool { return true }},
		wantPanic: "invalid transformer function",
	}, {
		label:     "Transformer",
		fnc:       Transformer,
		args:      []any{"", (func(int) uint)(nil)},
		wantPanic: "invalid transformer function",
	}, {
		label: "Transformer",
		fnc:   Transformer,
		args:  []any{"Func", func(Path) Path { return nil }},
	}, {
		label: "Transformer",
		fnc:   Transformer,
		args:  []any{"世界", func(int) bool { return true }},
	}, {
		label:     "Transformer",
		fnc:       Transformer,
		args:      []any{"/*", func(int) bool { return true }},
		wantPanic: "invalid name",
	}, {
		label: "Transformer",
		fnc:   Transformer,
		args:  []any{"_", func(int) bool { return true }},
	}, {
		label:     "FilterPath",
		fnc:       FilterPath,
		args:      []any{(func(Path) bool)(nil), Ignore()},
		wantPanic: "invalid path filter function",
	}, {
		label: "FilterPath",
		fnc:   FilterPath,
		args:  []any{func(Path) bool { return true }, Ignore()},
	}, {
		label:     "FilterPath",
		fnc:       FilterPath,
		args:      []any{func(Path) bool { return true }, Reporter(&defaultReporter{})},
		wantPanic: "invalid option type",
	}, {
		label: "FilterPath",
		fnc:   FilterPath,
		args:  []any{func(Path) bool { return true }, Options{Ignore(), Ignore()}},
	}, {
		label:     "FilterPath",
		fnc:       FilterPath,
		args:      []any{func(Path) bool { return true }, Options{Ignore(), Reporter(&defaultReporter{})}},
		wantPanic: "invalid option type",
	}, {
		label:     "FilterValues",
		fnc:       FilterValues,
		args:      []any{0, Ignore()},
		wantPanic: "invalid values filter function",
	}, {
		label: "FilterValues",
		fnc:   FilterValues,
		args:  []any{func(x, y int) bool { return true }, Ignore()},
	}, {
		label: "FilterValues",
		fnc:   FilterValues,
		args:  []any{func(x, y any) bool { return true }, Ignore()},
	}, {
		label:     "FilterValues",
		fnc:       FilterValues,
		args:      []any{func(x, y any) myBool { return true }, Ignore()},
		wantPanic: "invalid values filter function",
	}, {
		label:     "FilterValues",
		fnc:       FilterValues,
		args:      []any{func(x io.Reader, y any) bool { return true }, Ignore()},
		wantPanic: "invalid values filter function",
	}, {
		label:     "FilterValues",
		fnc:       FilterValues,
		args:      []any{(func(int, int) bool)(nil), Ignore()},
		wantPanic: "invalid values filter function",
	}, {
		label:     "FilterValues",
		fnc:       FilterValues,
		args:      []any{func(int, int) bool { return true }, Reporter(&defaultReporter{})},
		wantPanic: "invalid option type",
	}, {
		label: "FilterValues",
		fnc:   FilterValues,
		args:  []any{func(int, int) bool { return true }, Options{Ignore(), Ignore()}},
	}, {
		label:     "FilterValues",
		fnc:       FilterValues,
		args:      []any{func(int, int) bool { return true }, Options{Ignore(), Reporter(&defaultReporter{})}},
		wantPanic: "invalid option type",
	}}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			var gotPanic string
			func() {
				defer func() {
					if ex := recover(); ex != nil {
						if s, ok := ex.(string); ok {
							gotPanic = s
						} else {
							panic(ex)
						}
					}
				}()
				var vargs []reflect.Value
				for _, arg := range tt.args {
					vargs = append(vargs, reflect.ValueOf(arg))
				}
				reflect.ValueOf(tt.fnc).Call(vargs)
			}()
			if tt.wantPanic == "" {
				if gotPanic != "" {
					t.Fatalf("unexpected panic message: %s", gotPanic)
				}
			} else {
				if !strings.Contains(gotPanic, tt.wantPanic) {
					t.Fatalf("panic message:\ngot:  %s\nwant: %s", gotPanic, tt.wantPanic)
				}
			}
		})
	}
}
