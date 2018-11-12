// Copyright 2018, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmpopts

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
)

// DeepAllowUnexporeted creates a cmp.Option that recursively allows unexported
// fields in the passed types.
//
// NOTE: Prefer to explicitly allow structs with internal types via
// `cmp.AllowUnexported` rather than using this option. The philosophy behind this
// is that the test writer should know explicitly what they are comparing, and
// provide an explicit whitelist
func DeepAllowUnexported(vs ...interface{}) cmp.Option {
	m := make(map[reflect.Type]struct{})
	for _, v := range vs {
		structTypes(reflect.ValueOf(v), m)
	}
	var typs []interface{}
	for t := range m {
		typs = append(typs, reflect.New(t).Elem().Interface())
	}
	return cmp.AllowUnexported(typs...)
}

func structTypes(v reflect.Value, m map[reflect.Type]struct{}) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Ptr:
		if !v.IsNil() {
			structTypes(v.Elem(), m)
		}
	case reflect.Interface:
		if !v.IsNil() {
			structTypes(v.Elem(), m)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			structTypes(v.Index(i), m)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			structTypes(v.MapIndex(k), m)
		}
	case reflect.Struct:
		// Stop iterating if a cycle is detected. Structs with recursive types
		// causes a stack overflow in cmp.Equal if their values form a cycle but
		// DeepAllowUnexported should support them.
		if _, ok := m[v.Type()]; ok {
			return
		}
		m[v.Type()] = struct{}{}
		for i := 0; i < v.NumField(); i++ {
			structTypes(v.Field(i), m)
		}
	}
}
