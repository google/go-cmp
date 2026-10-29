// Copyright 2026, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmpopts_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type SimpleStruct struct {
	Alpha int
	Beta  string
	Gamma float64
}

type NestedStruct struct {
	Inner SimpleStruct
	Delta string
}

type EmbeddedStruct struct {
	SimpleStruct
	Epsilon string
}

func TestIgnoreFieldsExcept(t *testing.T) {
	t.Run("StructValue", func(t *testing.T) {
		x := SimpleStruct{Alpha: 1, Beta: "hello", Gamma: 1.5}
		y := SimpleStruct{Alpha: 1, Beta: "world", Gamma: 9.9}

		// When comparing only Alpha, differences in Beta and Gamma are ignored.
		if !cmp.Equal(x, y, cmpopts.IgnoreFieldsExcept(SimpleStruct{}, "Alpha")) {
			t.Errorf("unexpected difference when comparing only Alpha")
		}

		// If the excepted field differs, Equal reports false.
		yDiff := SimpleStruct{Alpha: 2, Beta: "world", Gamma: 9.9}
		if cmp.Equal(x, yDiff, cmpopts.IgnoreFieldsExcept(SimpleStruct{}, "Alpha")) {
			t.Errorf("expected difference when Alpha differs")
		}
	})

	t.Run("PointerToStruct", func(t *testing.T) {
		x := &SimpleStruct{Alpha: 10, Beta: "keep", Gamma: 1.0}
		y := &SimpleStruct{Alpha: 99, Beta: "keep", Gamma: 9.9}

		// Passing pointer-to-struct value to IgnoreFieldsExcept.
		optPtr := cmpopts.IgnoreFieldsExcept(&SimpleStruct{}, "Beta")
		if !cmp.Equal(x, y, optPtr) {
			t.Errorf("unexpected difference with pointer-to-struct option")
		}

		// Passing value struct option when comparing pointers.
		optVal := cmpopts.IgnoreFieldsExcept(SimpleStruct{}, "Beta")
		if !cmp.Equal(x, y, optVal) {
			t.Errorf("unexpected difference comparing pointers with value option")
		}

		// Comparing when excepted field differs on pointers.
		yDiff := &SimpleStruct{Alpha: 10, Beta: "different", Gamma: 1.0}
		if cmp.Equal(x, yDiff, optPtr) {
			t.Errorf("expected difference when Beta differs on pointers")
		}
	})

	t.Run("MultipleFields", func(t *testing.T) {
		x := SimpleStruct{Alpha: 1, Beta: "same", Gamma: 1.0}
		y := SimpleStruct{Alpha: 1, Beta: "same", Gamma: 2.0}

		if !cmp.Equal(x, y, cmpopts.IgnoreFieldsExcept(SimpleStruct{}, "Alpha", "Beta")) {
			t.Errorf("unexpected difference when Alpha and Beta match")
		}

		yDiff := SimpleStruct{Alpha: 2, Beta: "same", Gamma: 2.0}
		if cmp.Equal(x, yDiff, cmpopts.IgnoreFieldsExcept(SimpleStruct{}, "Alpha", "Beta")) {
			t.Errorf("expected difference when Alpha differs")
		}
	})

	t.Run("NestedFields", func(t *testing.T) {
		x := NestedStruct{Inner: SimpleStruct{Alpha: 5, Beta: "diff1"}, Delta: "diffA"}
		y := NestedStruct{Inner: SimpleStruct{Alpha: 5, Beta: "diff2"}, Delta: "diffB"}

		opt := cmpopts.IgnoreFieldsExcept(NestedStruct{}, "Inner.Alpha")
		if !cmp.Equal(x, y, opt) {
			t.Errorf("unexpected difference for nested field Inner.Alpha")
		}

		yDiff := NestedStruct{Inner: SimpleStruct{Alpha: 6, Beta: "diff2"}, Delta: "diffB"}
		if cmp.Equal(x, yDiff, opt) {
			t.Errorf("expected difference when Inner.Alpha differs")
		}
	})

	t.Run("EmbeddedFields", func(t *testing.T) {
		x := EmbeddedStruct{SimpleStruct: SimpleStruct{Alpha: 7, Beta: "diff"}, Epsilon: "diff1"}
		y := EmbeddedStruct{SimpleStruct: SimpleStruct{Alpha: 7, Beta: "other"}, Epsilon: "diff2"}

		// Promoted field name "Alpha"
		optPromoted := cmpopts.IgnoreFieldsExcept(EmbeddedStruct{}, "Alpha")
		if !cmp.Equal(x, y, optPromoted) {
			t.Errorf("unexpected difference using promoted field name")
		}

		// Qualified field name "SimpleStruct.Alpha"
		optQualified := cmpopts.IgnoreFieldsExcept(EmbeddedStruct{}, "SimpleStruct.Alpha")
		if !cmp.Equal(x, y, optQualified) {
			t.Errorf("unexpected difference using qualified field name")
		}
	})

	t.Run("EmptyNamesIgnoresAll", func(t *testing.T) {
		x := SimpleStruct{Alpha: 1, Beta: "a", Gamma: 1.0}
		y := SimpleStruct{Alpha: 2, Beta: "b", Gamma: 2.0}

		if !cmp.Equal(x, y, cmpopts.IgnoreFieldsExcept(SimpleStruct{})) {
			t.Errorf("expected all fields to be ignored when names is empty")
		}
	})
}

func TestIgnoreFieldsExceptPanic(t *testing.T) {
	tests := []struct {
		name      string
		typ       any
		fields    []string
		wantPanic string
	}{
		{
			name:      "NonStructInt",
			typ:       123,
			fields:    []string{"Alpha"},
			wantPanic: "must be a struct or pointer to struct",
		},
		{
			name:      "NilType",
			typ:       nil,
			fields:    []string{"Alpha"},
			wantPanic: "must be a struct or pointer to struct",
		},
		{
			name:      "PointerToNonStruct",
			typ:       new(int),
			fields:    []string{"Alpha"},
			wantPanic: "must be a struct or pointer to struct",
		},
		{
			name:      "NonExistentField",
			typ:       SimpleStruct{},
			fields:    []string{"NonExistent"},
			wantPanic: "does not exist",
		},
		{
			name:      "EmptyFieldName",
			typ:       SimpleStruct{},
			fields:    []string{""},
			wantPanic: "name must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic containing %q, got nil", tt.wantPanic)
				}
				msg := ""
				if s, ok := r.(string); ok {
					msg = s
				} else if err, ok := r.(error); ok {
					msg = err.Error()
				}
				if !strings.Contains(msg, tt.wantPanic) {
					t.Errorf("panic message %q does not contain %q", msg, tt.wantPanic)
				}
			}()

			cmpopts.IgnoreFieldsExcept(tt.typ, tt.fields...)
		})
	}
}
