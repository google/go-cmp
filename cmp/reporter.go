// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmp

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

// TODO: Can we leave the interface for a reporter here in the cmp package
// and somehow extract the implementation of defaultReporter into cmp/report?

type defaultReporter struct {
	Option
	diffs  []string // List of differences, possibly truncated
	ndiffs int      // Total number of differences
	nbytes int      // Number of bytes in diffs
	nlines int      // Number of lines in diffs
}

var _ reporter = (*defaultReporter)(nil)

func (r *defaultReporter) Report(x, y reflect.Value, eq bool, p Path) {
	// TODO: Is there a way to nicely print added/modified/removed elements
	// from a slice? This will most certainly require support from the
	// equality logic, but what would be the right API for this?
	//
	// The current API is equivalent to a Hamming distance for measuring the
	// difference between two sequences of symbols. That is, the only operation
	// we can represent is substitution. The new API would need to handle a
	// Levenshtein distance, such that insertions, deletions, and substitutions
	// are permitted. Furthermore, this will require an algorithm for computing
	// the edit distance. Unfortunately, the time complexity for a minimal
	// edit distance algorithm is not much better than O(n^2).
	// There are approximations for the algorithm that can run much faster.
	// See literature on computing Levenshtein distance.
	//
	// Passing in a pair of x and y is actually good for representing insertion
	// and deletion by the fact that x or y may be an invalid value. However,
	// we may need to pass in two paths px and py, to indicate the paths
	// relative to x and y. Alternative, since we only perform the Levenshtein
	// distance on slices, maybe we alter the SliceIndex type to record
	// two different indexes.

	// TODO: Perhaps we should coalesce differences on primitive kinds
	// together if the number of differences exceeds some ratio.
	// For example, comparing two SHA256s leads to many byte differences.

	if eq {
		// TODO: Maybe print some equal results for context?
		return // Ignore equal results
	}
	const maxBytes = 4096
	const maxLines = 256
	r.ndiffs++
	if r.nbytes < maxBytes && r.nlines < maxLines {
		sx := prettyPrint(x, true)
		sy := prettyPrint(y, true)
		if sx == sy {
			// Use of Stringer is not helpful, so rely on more exact formatting.
			sx = prettyPrint(x, false)
			sy = prettyPrint(y, false)
		}
		s := fmt.Sprintf("%#v:\n\t-: %s\n\t+: %s\n", p, sx, sy)
		r.diffs = append(r.diffs, s)
		r.nbytes += len(s)
		r.nlines += strings.Count(s, "\n")
	}
}

func (r *defaultReporter) String() string {
	s := strings.Join(r.diffs, "")
	if r.ndiffs == len(r.diffs) {
		return s
	}
	return fmt.Sprintf("%s... %d more differences ...", s, len(r.diffs)-r.ndiffs)
}

var stringerIface = reflect.TypeOf((*fmt.Stringer)(nil)).Elem()

func prettyPrint(v reflect.Value, useStringer bool) string {
	return formatAny(v, formatConfig{useStringer, true, true, true}, nil)
}

type formatConfig struct {
	useStringer    bool // Should the String method be used if available?
	printType      bool // Should we print the type before the value?
	followPointers bool // Should we recursively follow pointers?
	realPointers   bool // Should we print the real address of pointers?
}

// formatAny prints the value v in a pretty formatted manner.
// This is similar to fmt.Sprintf("%+v", v) except this:
//	* Prints the type unless it can be elided.
//	* Avoids printing struct fields that are zero.
//	* Prints a nil-slice as being nil, not empty.
//	* Prints map entries in deterministic order.
func formatAny(v reflect.Value, conf formatConfig, visited map[uintptr]bool) string {
	// TODO: Should this be a multi-line printout in certain situations?

	if !v.IsValid() {
		return "<non-existent>"
	}
	if conf.useStringer && v.Type().Implements(stringerIface) {
		if v.Kind() == reflect.Ptr && v.IsNil() {
			return "<nil>"
		}
		return fmt.Sprintf("%q", v.Interface().(fmt.Stringer).String())
	}

	switch v.Kind() {
	case reflect.Bool:
		return fmt.Sprint(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprint(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if v.Type().PkgPath() == "" || v.Kind() == reflect.Uintptr {
			return formatHex(v.Uint()) // Unnamed uints are usually bytes or words
		}
		return fmt.Sprint(v.Uint()) // Named uints are usually enumerations
	case reflect.Float32, reflect.Float64:
		return fmt.Sprint(v.Float())
	case reflect.Complex64, reflect.Complex128:
		return fmt.Sprint(v.Complex())
	case reflect.String:
		return fmt.Sprintf("%q", v)
	case reflect.UnsafePointer, reflect.Chan, reflect.Func:
		return formatPointer(v, conf)
	case reflect.Ptr:
		if v.IsNil() {
			if conf.printType {
				return fmt.Sprintf("(%v)(nil)", v.Type())
			}
			return "<nil>"
		}
		if visited[v.Pointer()] || !conf.followPointers {
			return formatPointer(v, conf)
		}
		visited = insertPointer(visited, v.Pointer())
		return "&" + formatAny(v.Elem(), conf, visited)
	case reflect.Interface:
		if v.IsNil() {
			if conf.printType {
				return fmt.Sprintf("%v(nil)", v.Type())
			}
			return "<nil>"
		}
		return formatAny(v.Elem(), conf, visited)
	case reflect.Slice:
		if v.IsNil() {
			if conf.printType {
				return fmt.Sprintf("%v(nil)", v.Type())
			}
			return "<nil>"
		}
		if visited[v.Pointer()] {
			return formatPointer(v, conf)
		}
		visited = insertPointer(visited, v.Pointer())
		fallthrough
	case reflect.Array:
		var ss []string
		subConf := conf
		subConf.printType = v.Type().Elem().Kind() == reflect.Interface
		for i := 0; i < v.Len(); i++ {
			s := formatAny(v.Index(i), subConf, visited)
			ss = append(ss, s)
		}
		s := fmt.Sprintf("{%s}", strings.Join(ss, ", "))
		if conf.printType {
			return v.Type().String() + s
		}
		return s
	case reflect.Map:
		if v.IsNil() {
			if conf.printType {
				return fmt.Sprintf("%v(nil)", v.Type())
			}
			return "<nil>"
		}
		if visited[v.Pointer()] {
			return formatPointer(v, conf)
		}
		visited = insertPointer(visited, v.Pointer())

		var ss []string
		subConf := conf
		subConf.printType = v.Type().Elem().Kind() == reflect.Interface
		for _, k := range sortKeys(v.MapKeys()) {
			sk := formatAny(k, formatConfig{realPointers: conf.realPointers}, visited)
			sv := formatAny(v.MapIndex(k), subConf, visited)
			ss = append(ss, fmt.Sprintf("%s: %s", sk, sv))
		}
		s := fmt.Sprintf("{%s}", strings.Join(ss, ", "))
		if conf.printType {
			return v.Type().String() + s
		}
		return s
	case reflect.Struct:
		var ss []string
		subConf := conf
		subConf.printType = true
		for i := 0; i < v.NumField(); i++ {
			vv := v.Field(i)
			if isZero(vv) {
				continue // Elide zero value fields
			}
			name := v.Type().Field(i).Name
			subConf.useStringer = conf.useStringer && isExported(name)
			s := formatAny(vv, subConf, visited)
			ss = append(ss, fmt.Sprintf("%s: %s", name, s))
		}
		s := fmt.Sprintf("{%s}", strings.Join(ss, ", "))
		if conf.printType {
			return v.Type().String() + s
		}
		return s
	default:
		panic(fmt.Sprintf("%v kind not handled", v.Kind()))
	}
}

func formatPointer(v reflect.Value, conf formatConfig) string {
	p := v.Pointer()
	if !conf.realPointers {
		p = 0 // For deterministic printing purposes
	}
	s := formatHex(uint64(p))
	if conf.printType {
		return fmt.Sprintf("(%v)(%s)", v.Type(), s)
	}
	return s
}

func formatHex(u uint64) string {
	var f string
	switch {
	case u <= 0xff:
		f = "0x%02x"
	case u <= 0xffff:
		f = "0x%04x"
	case u <= 0xffffff:
		f = "0x%06x"
	case u <= 0xffffffff:
		f = "0x%08x"
	case u <= 0xffffffffff:
		f = "0x%010x"
	case u <= 0xffffffffffff:
		f = "0x%012x"
	case u <= 0xffffffffffffff:
		f = "0x%014x"
	case u <= 0xffffffffffffffff:
		f = "0x%016x"
	}
	return fmt.Sprintf(f, u)
}

// insertPointer insert p into m, allocating m if necessary.
func insertPointer(m map[uintptr]bool, p uintptr) map[uintptr]bool {
	if m == nil {
		m = make(map[uintptr]bool)
	}
	m[p] = true
	return m
}

// isZero reports whether v is the zero value.
// This does not rely on Interface and so can be used on unexported fields.
func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool() == false
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Complex64, reflect.Complex128:
		return v.Complex() == 0
	case reflect.String:
		return v.String() == ""
	case reflect.UnsafePointer:
		return v.Pointer() == 0
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Ptr, reflect.Map, reflect.Slice:
		return v.IsNil()
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !isZero(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !isZero(v.Field(i)) {
				return false
			}
		}
		return true
	}
	return false
}

// isLess is a generic function for sorting arbitrary map keys.
// The inputs must be of the same type and must be comparable.
func isLess(x, y reflect.Value) bool {
	switch x.Type().Kind() {
	case reflect.Bool:
		return !x.Bool() && y.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return x.Int() < y.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return x.Uint() < y.Uint()
	case reflect.Float32, reflect.Float64:
		fx, fy := x.Float(), y.Float()
		return fx < fy || math.IsNaN(fx) && !math.IsNaN(fy)
	case reflect.Complex64, reflect.Complex128:
		cx, cy := x.Complex(), y.Complex()
		rx, ix, ry, iy := real(cx), imag(cx), real(cy), imag(cy)
		if rx == ry || (math.IsNaN(rx) && math.IsNaN(ry)) {
			return ix < iy || math.IsNaN(ix) && !math.IsNaN(iy)
		}
		return rx < ry || math.IsNaN(rx) && !math.IsNaN(ry)
	case reflect.Ptr, reflect.UnsafePointer, reflect.Chan:
		return x.Pointer() < y.Pointer()
	case reflect.String:
		return x.String() < y.String()
	case reflect.Array:
		for i := 0; i < x.Len(); i++ {
			if isLess(x.Index(i), y.Index(i)) {
				return true
			}
			if isLess(y.Index(i), x.Index(i)) {
				return false
			}
		}
		return false
	case reflect.Struct:
		for i := 0; i < x.NumField(); i++ {
			if isLess(x.Field(i), y.Field(i)) {
				return true
			}
			if isLess(y.Field(i), x.Field(i)) {
				return false
			}
		}
		return false
	case reflect.Interface:
		vx, vy := x.Elem(), y.Elem()
		if !vx.IsValid() || !vy.IsValid() {
			return !vx.IsValid() && vy.IsValid()
		}
		tx, ty := vx.Type(), vy.Type()
		if tx == ty {
			return isLess(x.Elem(), y.Elem())
		}
		if tx.Kind() != ty.Kind() {
			return vx.Kind() < vy.Kind()
		}
		if tx.String() != ty.String() {
			return tx.String() < ty.String()
		}
		if tx.PkgPath() != ty.PkgPath() {
			return tx.PkgPath() < ty.PkgPath()
		}
		// This can happen in rare situations, so we fallback to just comparing
		// the unique pointer for a reflect.Type. This guarantees deterministic
		// ordering within a program, but it is obviously not stable.
		return reflect.ValueOf(vx.Type()).Pointer() < reflect.ValueOf(vy.Type()).Pointer()
	default:
		// Must be Func, Map, or Slice; which are not comparable.
		panic(fmt.Sprintf("%T is not comparable", x.Type()))
	}
}

// sortKey sorts a list of map keys, deduplicating keys if necessary.
func sortKeys(vs []reflect.Value) []reflect.Value {
	if len(vs) == 0 {
		return vs
	}

	// Sort the map keys.
	sort.Sort(valueSorter(vs))

	// Deduplicate keys (fails for NaNs).
	vs2 := vs[:1]
	for _, v := range vs[1:] {
		if v.Interface() != vs2[len(vs2)-1].Interface() {
			vs2 = append(vs2, v)
		}
	}
	return vs2
}

// TODO: Use sort.Slice once Google AppEngine is on Go1.8 or above.
type valueSorter []reflect.Value

func (vs valueSorter) Len() int           { return len(vs) }
func (vs valueSorter) Less(i, j int) bool { return isLess(vs[i], vs[j]) }
func (vs valueSorter) Swap(i, j int)      { vs[i], vs[j] = vs[j], vs[i] }
