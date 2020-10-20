// Copyright 2020, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package value

import (
	"reflect"
	"testing"
)

func TestIsEmptyInterface(t *testing.T) {
	type (
		Empty      interface{}
		Exported   interface{ X() }
		Unexported interface{ x() }
	)
	tests := []struct {
		in   reflect.Type
		want bool
	}{
		{reflect.TypeOf((*interface{})(nil)).Elem(), true},
		{reflect.TypeOf((*Empty)(nil)).Elem(), true},
		{reflect.TypeOf((*Exported)(nil)).Elem(), false},
		{reflect.TypeOf((*Unexported)(nil)).Elem(), false},
		{reflect.TypeOf(5), false},
		{reflect.TypeOf(struct{}{}), false},
	}
	for _, tt := range tests {
		got := IsEmptyInterface(tt.in)
		if got != tt.want {
			t.Errorf("IsEmptyInterface(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
