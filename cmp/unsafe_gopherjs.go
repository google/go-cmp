// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

// +build js

package cmp

import (
	"reflect"

	"github.com/gopherjs/gopherjs/js"
)

const supportAllowUnexported = true

func unsafeRetrieveField(v reflect.Value, _ reflect.StructField) reflect.Value {
	// Constants copied from reflect/value.go.
	// Valid on versions of Go from 1.6 to 1.10, inclusive.
	const (
		flagStickyRO = 1 << 5
		flagEmbedRO  = 1 << 6
		flagRO       = flagStickyRO | flagEmbedRO
	)

	obj := js.InternalObject(v)
	obj.Set("flag", obj.Get("flag").Uint64()&^uint64(flagRO))
	return v
}
