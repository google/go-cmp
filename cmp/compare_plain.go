// Copyright 2026, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmp

import (
	"bytes"
	"reflect"
	"slices"
	"sync"
)

// plainTypes caches the result of [isPlainType] for each [reflect.Type].
var plainTypes sync.Map // map[reflect.Type]bool

// isPlainType reports whether values of type t can be compared with
// [plainEqual] instead of [state.compareAny]. Plain types reach no:
//   - Equal methods or interfaces, as compareAny may call Equal;
//   - unexported struct fields or possibly NaN map keys, as compareAny may panic;
//   - recursive types, as values may form cycles.
func isPlainType(t reflect.Type) bool {
	return checkPlainType(t, nil)
}

func checkPlainType(t reflect.Type, visiting []reflect.Type) (plain bool) {
	if plain, ok := plainTypes.Load(t); ok {
		return plain.(bool)
	}
	if slices.Contains(visiting, t) {
		return false // recursive type
	}
	visiting = append(visiting, t)
	defer func() { plainTypes.Store(t, plain) }()

	if _, ok := equalMethod(t); ok {
		return false
	}
	switch t.Kind() {
	case reflect.Interface:
		return false
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Name == "_" {
				continue
			}
			if !isExported(f.Name) || !checkPlainType(f.Type, visiting) {
				return false
			}
		}
	case reflect.Map:
		// Map keys are only used for lookups, so their types are not walked.
		return !mayBeNaN(t.Key()) && checkPlainType(t.Elem(), visiting)
	case reflect.Slice, reflect.Array, reflect.Ptr:
		return checkPlainType(t.Elem(), visiting)
	}
	return true
}

// mayBeNaN reports whether values of type t may be or contain NaN.
func mayBeNaN(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Interface:
		return true
	case reflect.Array:
		return mayBeNaN(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if mayBeNaN(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

// plainEqual reports whether vx and vy are equal following the same rules
// as [state.compareAny]. Their type must satisfy [isPlainType].
func plainEqual(vx, vy reflect.Value) bool {
	switch vx.Kind() {
	case reflect.Bool:
		return vx.Bool() == vy.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return vx.Int() == vy.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return vx.Uint() == vy.Uint()
	case reflect.Float32, reflect.Float64:
		return vx.Float() == vy.Float()
	case reflect.Complex64, reflect.Complex128:
		return vx.Complex() == vy.Complex()
	case reflect.String:
		return vx.String() == vy.String()
	case reflect.Chan, reflect.UnsafePointer:
		return vx.Pointer() == vy.Pointer()
	case reflect.Func:
		return vx.IsNil() && vy.IsNil()
	case reflect.Struct:
		for i := 0; i < vx.NumField(); i++ {
			fx := vx.Field(i)
			if !fx.CanInterface() {
				continue // blank field
			}
			if !plainEqual(fx, vy.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if vx.IsNil() || vy.IsNil() {
			return vx.IsNil() && vy.IsNil()
		}
		if vx.Type().Elem().Kind() == reflect.Uint8 {
			return bytes.Equal(vx.Bytes(), vy.Bytes())
		}
		fallthrough
	case reflect.Array:
		if vx.Len() != vy.Len() {
			return false
		}
		for i := 0; i < vx.Len(); i++ {
			if !plainEqual(vx.Index(i), vy.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if vx.IsNil() || vy.IsNil() {
			return vx.IsNil() && vy.IsNil()
		}
		if vx.Len() != vy.Len() {
			return false
		}
		t := vx.Type()
		k := reflect.New(t.Key()).Elem()
		ex := reflect.New(t.Elem()).Elem()
		iter := vx.MapRange()
		for iter.Next() {
			k.SetIterKey(iter)
			ex.SetIterValue(iter)
			ey := vy.MapIndex(k)
			if !ey.IsValid() || !plainEqual(ex, ey) {
				return false
			}
		}
		return true
	case reflect.Ptr:
		if vx.IsNil() || vy.IsNil() {
			return vx.IsNil() && vy.IsNil()
		}
		return plainEqual(vx.Elem(), vy.Elem())
	}
	panic("unreachable: " + vx.Kind().String())
}

// plainRootEqual compares x and y with [plainEqual] when possible without
// options, avoiding the setup of a state. ok reports whether it did.
func plainRootEqual(x, y any, opts []Option) (eq, ok bool) {
	if len(opts) > 0 {
		return false, false
	}
	vx, vy := reflect.ValueOf(x), reflect.ValueOf(y)
	if !vx.IsValid() || !vy.IsValid() || vx.Type() != vy.Type() || !isPlainType(vx.Type()) {
		return false, false
	}
	return plainEqual(vx, vy), true
}
