// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmpopts

import (
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/internal/function"
)

// DiscardElements transforms slices and maps by discarding some elements.
// The remove function must be of the form "func(T) bool" where it reports true
// for any element that should be discarded. This transforms any slices and maps
// of type []V or map[K]V, where type V is assignable to type T.
//
// As an example, zero elements in a []MyStruct can be discarded with:
//	DiscardElements(func(v MyStruct) bool { return v == MyStruct{} })
//
// DiscardElements can be used in conjunction with EquateEmpty,
// but cannot be used with SortMaps.
func DiscardElements(rm interface{}) cmp.Option {
	vf := reflect.ValueOf(rm)
	if !function.IsType(vf.Type(), function.Remove) || vf.IsNil() {
		panic(fmt.Sprintf("invalid remove function: %T", rm))
	}
	d := discarder{vf.Type().In(0), vf}
	return cmp.FilterValues(d.filter, cmp.Transformer("Discard", d.discard))
}

type discarder struct {
	in  reflect.Type  // T
	fnc reflect.Value // func(T) bool
}

func (d discarder) filter(x, y interface{}) bool {
	vx := reflect.ValueOf(x)
	vy := reflect.ValueOf(y)
	if x == nil || y == nil || vx.Type() != vy.Type() ||
		!(vx.Kind() == reflect.Slice || vx.Kind() == reflect.Map) ||
		!vx.Type().Elem().AssignableTo(d.in) || vx.Len()+vy.Len() == 0 {
		return false
	}
	ok := d.hasDiscardable(vx) || d.hasDiscardable(vy)
	return ok
}
func (d discarder) hasDiscardable(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if d.fnc.Call([]reflect.Value{v.Index(i)})[0].Bool() {
				return true
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if d.fnc.Call([]reflect.Value{v.MapIndex(k)})[0].Bool() {
				return true
			}
		}
	}
	return false
}
func (d discarder) discard(x interface{}) interface{} {
	src := reflect.ValueOf(x)
	switch src.Kind() {
	case reflect.Slice:
		dst := reflect.MakeSlice(src.Type(), 0, src.Len())
		for i := 0; i < src.Len(); i++ {
			v := src.Index(i)
			if !d.fnc.Call([]reflect.Value{v})[0].Bool() {
				dst = reflect.Append(dst, v)
			}
		}
		return dst.Interface()
	case reflect.Map:
		dst := reflect.MakeMap(src.Type())
		for _, k := range src.MapKeys() {
			v := src.MapIndex(k)
			if !d.fnc.Call([]reflect.Value{v})[0].Bool() {
				dst.SetMapIndex(k, v)
			}
		}
		return dst.Interface()
	}
	panic("not a slice or map") // Not possible due to FilterValues
}
