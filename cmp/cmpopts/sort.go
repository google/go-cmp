// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmpopts

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/internal/function"
)

// SortSlices returns a Transformer option that sorts all []V.
//
// If less is nil, GenericLess will be used, which works on any comparable element type.
//
// Otherwise, the less function must be of the form "func(T, T) bool" which is used to
// sort any slice with element type V that is assignable to T.
//
// The less function must be:
//	• Deterministic: less(x, y) == less(x, y)
//	• Irreflexive: !less(x, x)
//	• Transitive: if !less(x, y) and !less(y, z), then !less(x, z)
//
// The less function does not have to be "total". That is, if !less(x, y) and
// !less(y, x) for two elements x and y, their relative order is maintained.
//
// SortSlices can be used in conjunction with EquateEmpty.
func SortSlices(less interface{}) cmp.Option {
	if less == nil {
		less = GenericLess
	}
	vf := reflect.ValueOf(less)
	if !function.IsType(vf.Type(), function.Less) || vf.IsNil() {
		panic(fmt.Sprintf("invalid less function: %T", less))
	}
	ss := sliceSorter{vf.Type().In(0), vf}
	return cmp.FilterValues(ss.filter, cmp.Transformer("Sort", ss.sort))
}

type sliceSorter struct {
	in  reflect.Type  // T
	fnc reflect.Value // func(T, T) bool
}

func (ss sliceSorter) filter(x, y interface{}) bool {
	vx, vy := reflect.ValueOf(x), reflect.ValueOf(y)
	if !(x != nil && y != nil && vx.Type() == vy.Type()) ||
		!(vx.Kind() == reflect.Slice && vx.Type().Elem().AssignableTo(ss.in)) ||
		(vx.Len() <= 1 && vy.Len() <= 1) {
		return false
	}
	// Check whether the slices are already sorted to avoid an infinite
	// recursion cycle applying the same transform to itself.
	ok1 := sliceIsSorted(x, func(i, j int) bool { return ss.less(vx, i, j) })
	ok2 := sliceIsSorted(y, func(i, j int) bool { return ss.less(vy, i, j) })
	return !ok1 || !ok2
}
func (ss sliceSorter) sort(x interface{}) interface{} {
	src := reflect.ValueOf(x)
	dst := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
	for i := 0; i < src.Len(); i++ {
		dst.Index(i).Set(src.Index(i))
	}
	sortSliceStable(dst.Interface(), func(i, j int) bool { return ss.less(dst, i, j) })
	ss.checkSort(dst)
	return dst.Interface()
}
func (ss sliceSorter) checkSort(v reflect.Value) {
	start := -1 // Start of a sequence of equal elements.
	for i := 1; i < v.Len(); i++ {
		if ss.less(v, i-1, i) {
			// Check that first and last elements in v[start:i] are equal.
			if start >= 0 && (ss.less(v, start, i-1) || ss.less(v, i-1, start)) {
				panic(fmt.Sprintf("incomparable values detected: want equal elements: %v", v.Slice(start, i)))
			}
			start = -1
		} else if start == -1 {
			start = i
		}
	}
}
func (ss sliceSorter) less(v reflect.Value, i, j int) bool {
	vx, vy := v.Index(i), v.Index(j)
	return ss.fnc.Call([]reflect.Value{vx, vy})[0].Bool()
}

// SortMaps returns a Transformer option that flattens map[K]V types to be a
// sorted []struct{K, V}. The less function must be of the form
// "func(T, T) bool" which is used to sort any map with key K that is
// assignable to T.
//
// Flattening the map into a slice has the property that cmp.Equal is able to
// use Comparers on K or the K.Equal method if it exists.
//
// The less function must be:
//	• Deterministic: less(x, y) == less(x, y)
//	• Irreflexive: !less(x, x)
//	• Transitive: if !less(x, y) and !less(y, z), then !less(x, z)
//	• Total: if x != y, then either less(x, y) or less(y, x)
//
// SortMaps can be used in conjunction with EquateEmpty.
func SortMaps(less interface{}) cmp.Option {
	if less == nil {
		less = GenericLess
	}
	vf := reflect.ValueOf(less)
	if !function.IsType(vf.Type(), function.Less) || vf.IsNil() {
		panic(fmt.Sprintf("invalid less function: %T", less))
	}
	ms := mapSorter{vf.Type().In(0), vf}
	return cmp.FilterValues(ms.filter, cmp.Transformer("Sort", ms.sort))
}

type mapSorter struct {
	in  reflect.Type  // T
	fnc reflect.Value // func(T, T) bool
}

func (ms mapSorter) filter(x, y interface{}) bool {
	vx, vy := reflect.ValueOf(x), reflect.ValueOf(y)
	return (x != nil && y != nil && vx.Type() == vy.Type()) &&
		(vx.Kind() == reflect.Map && vx.Type().Key().AssignableTo(ms.in)) &&
		(vx.Len() != 0 || vy.Len() != 0)
}
func (ms mapSorter) sort(x interface{}) interface{} {
	src := reflect.ValueOf(x)
	outType := mapEntryType(src.Type())
	dst := reflect.MakeSlice(reflect.SliceOf(outType), src.Len(), src.Len())
	for i, k := range src.MapKeys() {
		v := reflect.New(outType).Elem()
		v.Field(0).Set(k)
		v.Field(1).Set(src.MapIndex(k))
		dst.Index(i).Set(v)
	}
	sortSlice(dst.Interface(), func(i, j int) bool { return ms.less(dst, i, j) })
	ms.checkSort(dst)
	return dst.Interface()
}
func (ms mapSorter) checkSort(v reflect.Value) {
	for i := 1; i < v.Len(); i++ {
		if !ms.less(v, i-1, i) {
			panic(fmt.Sprintf("partial order detected: want %v < %v", v.Index(i-1), v.Index(i)))
		}
	}
}
func (ms mapSorter) less(v reflect.Value, i, j int) bool {
	vx, vy := v.Index(i).Field(0), v.Index(j).Field(0)
	if !hasReflectStructOf {
		vx, vy = vx.Elem(), vy.Elem()
	}
	return ms.fnc.Call([]reflect.Value{vx, vy})[0].Bool()
}

// GenericLess reports whether x is less than y, where both x and y must
// be comparable types. For basic types, the values are compared by
// value (pointer values are compared by pointer value, so ordering is
// runtime-dependent in this case); for composite types, values are
// compared lexicographically by component. Float and complex NaN values
// will be compared as equal to avoid potential panics when sorting.
//
// GenericLess panics if provided with non-comparable values.
func GenericLess(x, y interface{}) bool {
	return cmpVal(reflect.ValueOf(x), reflect.ValueOf(y)) < 0
}

func cmpVal(xv, yv reflect.Value) int {
	if xvalid, yvalid := xv.IsValid(), yv.IsValid(); !xvalid || !yvalid {
		// nil sorts before anything else.
		switch {
		case yvalid:
			return -1
		case xvalid:
			return 1
		}
		return 0
	}
	xt := xv.Type()
	yt := yv.Type()
	if xt != yt {
		// Comparing by strings gives fairly predictable results
		// in the majority of cases.
		c := strings.Compare(xt.String(), yt.String())
		if c != 0 || xt == yt {
			return c
		}
		// String comparison has failed, so fall back to type-pointer comparison.
		return cmpVal(reflect.ValueOf(xt), reflect.ValueOf(yt))
	}
	switch xt.Kind() {
	case reflect.String:
		return strings.Compare(xv.String(), yv.String())
	case reflect.Bool:
		xb, yb := xv.Bool(), yv.Bool()
		switch {
		case xb == yb:
			return 0
		case !xb:
			return -1
		}
		return 1
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		xi, yi := xv.Int(), yv.Int()
		switch {
		case xi < yi:
			return -1
		case xi > yi:
			return 1
		}
		return 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		xi, yi := xv.Uint(), yv.Uint()
		switch {
		case xi < yi:
			return -1
		case xi > yi:
			return 1
		}
		return 0
	case reflect.Float32, reflect.Float64:
		xf, yf := xv.Float(), yv.Float()
		if xnan, ynan := math.IsNaN(xf), math.IsNaN(yf); xnan || ynan {
			switch {
			case xnan && ynan:
				return 0
			case xnan && !ynan:
				return -1
			}
			return 1
		}
		switch {
		case xf < yf:
			return -1
		case xf > yf:
			return 1
		}
		return 0
	case reflect.Complex64, reflect.Complex128:
		xc, yc := xv.Complex(), yv.Complex()
		if c := cmpVal(reflect.ValueOf(real(xc)), reflect.ValueOf(real(yc))); c != 0 {
			return c
		}
		return cmpVal(reflect.ValueOf(imag(xc)), reflect.ValueOf(imag(yc)))
	case reflect.Struct:
		nf := xt.NumField()
		for i := 0; i < nf; i++ {
			if c := cmpVal(xv.Field(i), yv.Field(i)); c != 0 {
				return c
			}
		}
		return 0
	case reflect.Array:
		len := xv.Len()
		for i := 0; i < len; i++ {
			if c := cmpVal(xv.Index(i), yv.Index(i)); c != 0 {
				return c
			}
		}
		return 0
	case reflect.Ptr, reflect.Chan, reflect.UnsafePointer:
		xi, yi := xv.Pointer(), yv.Pointer()
		switch {
		case xi < yi:
			return -1
		case xi > yi:
			return 1
		}
		return 0
	case reflect.Interface:
		return cmpVal(xv.Elem(), yv.Elem())
	case reflect.Func, reflect.Map, reflect.Slice:
		panic(fmt.Errorf("cannot compare uncomparable type %v", xt))
	default:
		panic(fmt.Errorf("kind %v unimplemented", xt.Kind()))
	}
}
