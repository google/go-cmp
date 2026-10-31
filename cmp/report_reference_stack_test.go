// Copyright 2026, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmp

import (
	"reflect"
	"testing"
)

func TestPointerReferencesSplit(t *testing.T) {
	left, right := new(int), new(int)
	var parent pointerReferences
	pair := parent.PushPair(reflect.ValueOf(left), reflect.ValueOf(right), diffUnknown, false)
	x, y := parent.Split()
	if _, seen := x.Push(reflect.ValueOf(right)); seen {
		t.Fatal("left view treated a right-only ancestor as its own cycle")
	}
	if _, seen := x.Push(reflect.ValueOf(right)); !seen {
		t.Fatal("left view missed a newly pushed ancestor")
	}
	x.Pop()
	if _, seen := x.Push(reflect.ValueOf(left)); !seen {
		t.Fatal("left view missed its own original ancestor")
	}
	if _, seen := y.Push(reflect.ValueOf(left)); seen {
		t.Fatal("right view treated a left-only ancestor as its own cycle")
	}
	y.Pop()
	if _, seen := y.Push(reflect.ValueOf(right)); !seen {
		t.Fatal("right view missed its own original ancestor")
	}
	if len(parent.stack) != 1 || parent.stack[0] != pair {
		t.Fatal("formatting a side changed the parent stack")
	}
	if _, seen := parent.Push(reflect.ValueOf(left)); !seen {
		t.Fatal("paired parent lost the left ancestor")
	}
	if _, seen := parent.Push(reflect.ValueOf(right)); !seen {
		t.Fatal("paired parent lost the right ancestor")
	}
}
