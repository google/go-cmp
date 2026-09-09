// Copyright 2019, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package function

import (
	"bytes"
	"reflect"
	"testing"
)

type myType struct{ bytes.Buffer }

func (myType) valueMethod() {}
func (myType) ValueMethod() {}

func (*myType) pointerMethod() {}
func (*myType) PointerMethod() {}

func TestNameOf(t *testing.T) {
	tests := []struct {
		fnc  any
		want []string // any matching entry is ok
	}{
		{TestNameOf, []string{"function.TestNameOf"}},
		{func() {}, []string{"function.TestNameOf.func1"}},
		{(myType).valueMethod, []string{"function.myType.valueMethod"}},
		{(myType).ValueMethod, []string{"function.myType.ValueMethod"}},
		{(myType{}).valueMethod, []string{"function.myType.valueMethod"}},
		{(myType{}).ValueMethod, []string{"function.myType.ValueMethod"}},
		{(*myType).valueMethod, []string{"function.myType.valueMethod"}},
		{(*myType).ValueMethod, []string{"function.myType.ValueMethod"}},
		{(&myType{}).valueMethod, []string{"function.myType.valueMethod"}},
		{(&myType{}).ValueMethod, []string{"function.myType.ValueMethod"}},
		{(*myType).pointerMethod, []string{"function.myType.pointerMethod"}},
		{(*myType).PointerMethod, []string{"function.myType.PointerMethod"}},
		{(&myType{}).pointerMethod, []string{"function.myType.pointerMethod"}},
		{(&myType{}).PointerMethod, []string{"function.myType.PointerMethod"}},
		// Starting in Go 1.28, wrappers of embedded methods have the
		// name of the wrapped type rather than the wrapping type.
		{(*myType).Write, []string{"function.myType.Write", "bytes.Buffer.Write"}},
		{(&myType{}).Write, []string{"bytes.Buffer.Write"}},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := NameOf(reflect.ValueOf(tt.fnc))
			match := false
			for _, w := range tt.want {
				if got == w {
					match = true
				}
			}
			if !match {
				t.Errorf("NameOf() = %v, want %v", got, tt.want)
			}
		})
	}
}
