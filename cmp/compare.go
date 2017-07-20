// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

// Package cmp determines equality of values.
//
// This package is intended to be a more powerful and safer alternative to
// reflect.DeepEqual for comparing whether two values are semantically equal.
//
// The primary features of cmp are:
//
// • When the default behavior of equality does not suit the needs of the test,
// custom equality functions can override the equality operation.
// For example, an equality function may report floats as equal so long as they
// are within some tolerance of each other.
//
// • Types that have an Equal method may use that method to determine equality.
// This allows package authors to determine the equality operation for the types
// that they define.
//
// • If no custom equality functions are used and no Equal method is defined,
// equality is determined by recursively comparing the primitive kinds on both
// values, much like reflect.DeepEqual. Unlike reflect.DeepEqual, unexported
// fields are not compared by default; they result in panics unless suppressed
// by using an Ignore option (see cmpopts.IgnoreUnexported) or explictly compared
// using the AllowUnexported option.
package cmp

import (
	"fmt"
	"reflect"
)

// BUG: Maps with keys containing NaN values cannot be properly compared due to
// the reflection package's inability to retrieve such entries. Equal will panic
// anytime it comes across a NaN key, but this behavior may change.
//
// See https://golang.org/issue/11104 for more details.

// Equal reports whether x and y are equal by recursively applying the
// following rules in the given order to x and y and all of their sub-values:
//
// • If two values are not of the same type, then they are never equal
// and the overall result is false.
//
// • Let S be the set of all Ignore, Transformer, and Comparer options that
// remain after applying all path filters, value filters, and type filters.
// If at least one Ignore exists in S, then the comparison is ignored.
// If the number of Transformer and Comparer options in S is greater than one,
// then Equal panics because it is ambiguous which option to use.
// If S contains a single Transformer, then apply that transformer on the
// current values and recursively call Equal on the transformed output values.
// If S contains a single Comparer, then use that Comparer to determine whether
// the current values are equal or not.
// Otherwise, S is empty and evaluation proceeds to the next rule.
//
// • If the values have an Equal method of the form "(T) Equal(T) bool" or
// "(T) Equal(I) bool" where T is assignable to I, then use the result of
// x.Equal(y). Otherwise, no such method exists and evaluation proceeds to
// the next rule.
//
// • Lastly, try to compare x and y based on their basic kinds.
// Simple kinds like booleans, integers, floats, complex numbers, strings, and
// channels are compared using the equivalent of the == operator in Go.
// Functions are only equal if they are both nil, otherwise they are unequal.
// Pointers are equal if the underlying values they point to are also equal.
// Interfaces are equal if their underlying concrete values are also equal.
//
// Structs are equal if all of their fields are equal. If a struct contains
// unexported fields, Equal panics unless the AllowUnexported option is used or
// an Ignore option (e.g., cmpopts.IgnoreUnexported) ignores that field.
//
// Arrays, slices, and maps are equal if they are both nil or both non-nil
// with the same length and the elements at each index or key are equal.
// Note that a non-nil empty slice and a nil slice are not equal.
// To equate empty slices and maps, consider using cmpopts.EquateEmpty.
// Map keys are equal according to the == operator.
// To use custom comparisons for map keys, consider using cmpopts.SortMaps.
func Equal(x, y interface{}, opts ...Option) bool {
	s := newState(opts)
	s.compareAny(reflect.ValueOf(x), reflect.ValueOf(y))
	return s.eq
}

// Diff returns a human-readable report of the differences between two values.
// It returns an empty string if and only if Equal returns true for the same
// input values and options. The output string will use the "-" symbol to
// indicate elements removed from x, and the "+" symbol to indicate elements
// added to y.
//
// Do not depend on this output being stable.
func Diff(x, y interface{}, opts ...Option) string {
	r := new(defaultReporter)
	opts = append(opts[:len(opts):len(opts)], r) // Force copy when appending
	eq := Equal(x, y, opts...)
	d := r.String()
	if (d == "") != eq {
		panic("inconsistent difference and equality results")
	}
	return d
}

type state struct {
	eq      bool // Current result of comparison
	curPath Path // The current path in the value tree

	// dsCheck tracks the state needed to periodically perform checks that
	// user provided func(T, T) bool functions are symmetric and deterministic.
	//
	// Checks occur every Nth function call, where N is a triangular number:
	//	0 1 3 6 10 15 21 28 36 45 55 66 78 91 105 120 136 153 171 190 ...
	// See https://en.wikipedia.org/wiki/Triangular_number
	//
	// This sequence ensures that the cost of checks drops significantly as
	// the number of functions calls grows larger.
	dsCheck struct{ curr, next int }

	// These fields, once set by processOption, will not change.
	exporters map[reflect.Type]bool // Set of structs with unexported field visibility
	optsIgn   []option              // List of all ignore options without value filters
	opts      []option              // List of all other options
	reporter  reporter              // Optional reporter used for difference formatting
}

func newState(opts []Option) *state {
	s := &state{eq: true}
	for _, opt := range opts {
		s.processOption(opt)
	}
	// Move Ignore options to the front so that they are evaluated first.
	for i, j := 0, 0; i < len(s.opts); i++ {
		if s.opts[i].op == nil {
			s.opts[i], s.opts[j] = s.opts[j], s.opts[i]
			j++
		}
	}
	return s
}

func (s *state) processOption(opt Option) {
	switch opt := opt.(type) {
	case Options:
		for _, o := range opt {
			s.processOption(o)
		}
	case visibleStructs:
		if s.exporters == nil {
			s.exporters = make(map[reflect.Type]bool)
		}
		for t := range opt {
			s.exporters[t] = true
		}
	case option:
		if opt.typeFilter == nil && len(opt.pathFilters)+len(opt.valueFilters) == 0 {
			panic(fmt.Sprintf("cannot use an unfiltered option: %v", opt))
		}
		if opt.op == nil && len(opt.valueFilters) == 0 {
			s.optsIgn = append(s.optsIgn, opt)
		} else {
			s.opts = append(s.opts, opt)
		}
	case reporter:
		if s.reporter != nil {
			panic("difference reporter already registered")
		}
		s.reporter = opt
	default:
		panic(fmt.Sprintf("unknown option %T", opt))
	}
}

func (s *state) compareAny(vx, vy reflect.Value) {
	// TODO: Support cyclic data structures.

	// Rule 0: Differing types are never equal.
	if !vx.IsValid() || !vy.IsValid() {
		s.report(vx.IsValid() == vy.IsValid(), vx, vy)
		return
	}
	if vx.Type() != vy.Type() {
		s.report(false, vx, vy) // Possible for path to be empty
		return
	}
	t := vx.Type()
	if len(s.curPath) == 0 {
		s.curPath.push(&pathStep{typ: t})
	}

	// Rule 1: Check whether an option applies on this node in the value tree.
	if s.tryOptions(&vx, &vy, t) {
		return
	}

	// Rule 2: Check whether the type has a valid Equal method.
	if s.tryMethod(vx, vy, t) {
		return
	}

	// Rule 3: Recursively descend into each value's underlying kind.
	switch t.Kind() {
	case reflect.Bool:
		s.report(vx.Bool() == vy.Bool(), vx, vy)
		return
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s.report(vx.Int() == vy.Int(), vx, vy)
		return
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		s.report(vx.Uint() == vy.Uint(), vx, vy)
		return
	case reflect.Float32, reflect.Float64:
		s.report(vx.Float() == vy.Float(), vx, vy)
		return
	case reflect.Complex64, reflect.Complex128:
		s.report(vx.Complex() == vy.Complex(), vx, vy)
		return
	case reflect.String:
		s.report(vx.String() == vy.String(), vx, vy)
		return
	case reflect.Chan, reflect.UnsafePointer:
		s.report(vx.Pointer() == vy.Pointer(), vx, vy)
		return
	case reflect.Func:
		s.report(vx.IsNil() && vy.IsNil(), vx, vy)
		return
	case reflect.Ptr:
		if vx.IsNil() || vy.IsNil() {
			s.report(vx.IsNil() && vy.IsNil(), vx, vy)
			return
		}
		s.curPath.push(&indirect{pathStep{t.Elem()}})
		defer s.curPath.pop()
		s.compareAny(vx.Elem(), vy.Elem())
		return
	case reflect.Interface:
		if vx.IsNil() || vy.IsNil() {
			s.report(vx.IsNil() && vy.IsNil(), vx, vy)
			return
		}
		if vx.Elem().Type() != vy.Elem().Type() {
			s.report(false, vx.Elem(), vy.Elem())
			return
		}
		s.curPath.push(&typeAssertion{pathStep{vx.Elem().Type()}})
		defer s.curPath.pop()
		s.compareAny(vx.Elem(), vy.Elem())
		return
	case reflect.Slice:
		if vx.IsNil() || vy.IsNil() {
			s.report(vx.IsNil() && vy.IsNil(), vx, vy)
			return
		}
		fallthrough
	case reflect.Array:
		s.compareArray(vx, vy, t)
		return
	case reflect.Map:
		s.compareMap(vx, vy, t)
		return
	case reflect.Struct:
		s.compareStruct(vx, vy, t)
		return
	default:
		panic(fmt.Sprintf("%v kind not handled", t.Kind()))
	}
}

// tryOptions iterates through all of the options and evaluates whether any
// of them can be applied. This may modify the underlying values vx and vy
// if an unexported field is being forcibly exported.
func (s *state) tryOptions(vx, vy *reflect.Value, t reflect.Type) bool {
	// Try all ignore options that do not depend on the value first.
	// This avoids possible panics when processing unexported fields.
	for _, opt := range s.optsIgn {
		var v reflect.Value // Dummy value; should never be used
		if s.applyFilters(v, v, t, opt) {
			return true // Ignore option applied
		}
	}

	// Since the values must be used after this point, verify that the values
	// are either exported or can be forcibly exported.
	if sf, ok := s.curPath[len(s.curPath)-1].(*structField); ok && sf.unexported {
		if !sf.force {
			const help = "consider using AllowUnexported or cmpopts.IgnoreUnexported"
			panic(fmt.Sprintf("cannot handle unexported field: %#v\n%s", s.curPath, help))
		}

		// Use unsafe pointer arithmetic to get read-write access to an
		// unexported field in the struct.
		*vx = unsafeRetrieveField(sf.pvx, sf.field)
		*vy = unsafeRetrieveField(sf.pvy, sf.field)
	}

	// Try all other options now.
	optIdx := -1 // Index of Option to apply
	for i, opt := range s.opts {
		if !s.applyFilters(*vx, *vy, t, opt) {
			continue
		}
		if opt.op == nil {
			return true // Ignored comparison
		}
		if optIdx >= 0 {
			panic(fmt.Sprintf("ambiguous set of options at %#v\n\n%v\n\n%v\n", s.curPath, s.opts[optIdx], opt))
		}
		optIdx = i
	}
	if optIdx >= 0 {
		s.applyOption(*vx, *vy, t, s.opts[optIdx])
		return true
	}
	return false
}

func (s *state) applyFilters(vx, vy reflect.Value, t reflect.Type, opt option) bool {
	if opt.typeFilter != nil {
		if !t.AssignableTo(opt.typeFilter) {
			return false
		}
	}
	for _, f := range opt.pathFilters {
		if !f(s.curPath) {
			return false
		}
	}
	for _, f := range opt.valueFilters {
		if !t.AssignableTo(f.in) || !s.callFunc(f.fnc, vx, vy) {
			return false
		}
	}
	return true
}

func (s *state) applyOption(vx, vy reflect.Value, t reflect.Type, opt option) {
	switch op := opt.op.(type) {
	case *transformer:
		vx = op.fnc.Call([]reflect.Value{vx})[0]
		vy = op.fnc.Call([]reflect.Value{vy})[0]
		s.curPath.push(&transform{pathStep{op.fnc.Type().Out(0)}, op})
		defer s.curPath.pop()
		s.compareAny(vx, vy)
		return
	case *comparer:
		eq := s.callFunc(op.fnc, vx, vy)
		s.report(eq, vx, vy)
		return
	}
}

func (s *state) tryMethod(vx, vy reflect.Value, t reflect.Type) bool {
	// Check if this type even has an Equal method.
	m, ok := t.MethodByName("Equal")
	ft := functionType(m.Type)
	if !ok || (ft != equalFunc && ft != equalIfaceFunc) {
		return false
	}

	eq := s.callFunc(m.Func, vx, vy)
	s.report(eq, vx, vy)
	return true
}

func (s *state) callFunc(f, x, y reflect.Value) bool {
	got := f.Call([]reflect.Value{x, y})[0].Bool()
	if s.dsCheck.curr == s.dsCheck.next {
		// Swapping the input arguments is sufficient to check that
		// f is symmetric and deterministic.
		want := f.Call([]reflect.Value{y, x})[0].Bool()
		if got != want {
			fn := getFuncName(f.Pointer())
			panic(fmt.Sprintf("non-deterministic or non-symmetric function detected: %s", fn))
		}
		s.dsCheck.curr = 0
		s.dsCheck.next++
	}
	s.dsCheck.curr++
	return got
}

func (s *state) compareArray(vx, vy reflect.Value, t reflect.Type) {
	step := &sliceIndex{pathStep{t.Elem()}, 0}
	s.curPath.push(step)
	defer s.curPath.pop()

	// Regardless of the lengths, we always try to compare the elements.
	// If one slice is longer, we will report the elements of the longer
	// slice as different (relative to an invalid reflect.Value).
	nmin := vx.Len()
	if nmin > vy.Len() {
		nmin = vy.Len()
	}
	for i := 0; i < nmin; i++ {
		step.key = i
		s.compareAny(vx.Index(i), vy.Index(i))
	}
	for i := nmin; i < vx.Len(); i++ {
		step.key = i
		s.report(false, vx.Index(i), reflect.Value{})
	}
	for i := nmin; i < vy.Len(); i++ {
		step.key = i
		s.report(false, reflect.Value{}, vy.Index(i))
	}
}

func (s *state) compareMap(vx, vy reflect.Value, t reflect.Type) {
	if vx.IsNil() || vy.IsNil() {
		s.report(vx.IsNil() && vy.IsNil(), vx, vy)
		return
	}

	// We combine and sort the two map keys so that we can perform the
	// comparisons in a deterministic order.
	step := &mapIndex{pathStep: pathStep{t.Elem()}}
	s.curPath.push(step)
	defer s.curPath.pop()
	for _, k := range sortKeys(append(vx.MapKeys(), vy.MapKeys()...)) {
		step.key = k
		vvx := vx.MapIndex(k)
		vvy := vy.MapIndex(k)
		switch {
		case vvx.IsValid() && vvy.IsValid():
			s.compareAny(vvx, vvy)
		case vvx.IsValid() && !vvy.IsValid():
			s.report(false, vvx, reflect.Value{})
		case !vvx.IsValid() && vvy.IsValid():
			s.report(false, reflect.Value{}, vvy)
		default:
			// It is possible for both vvx and vvy to be invalid if the
			// key contained a NaN value in it. There is no way in
			// reflection to be able to retrieve these values.
			// See https://golang.org/issue/11104
			panic(fmt.Sprintf("%#v has map key with NaNs", s.curPath))
		}
	}
}

func (s *state) compareStruct(vx, vy reflect.Value, t reflect.Type) {
	var vax, vay reflect.Value // Addressable versions of vx and vy

	step := &structField{}
	s.curPath.push(step)
	defer s.curPath.pop()
	for i := 0; i < t.NumField(); i++ {
		vvx := vx.Field(i)
		vvy := vy.Field(i)
		step.typ = t.Field(i).Type
		step.name = t.Field(i).Name
		step.idx = i
		step.unexported = !isExported(step.name)
		if step.unexported {
			// Defer checking of unexported fields until later to give an
			// Ignore a chance to ignore the field.
			if !vax.IsValid() || !vay.IsValid() {
				// For unsafeRetrieveField to work, the parent struct must
				// be addressable. Create a new copy of the values if
				// necessary to make them addressable.
				vax = makeAddressable(vx)
				vay = makeAddressable(vy)
			}
			step.force = s.exporters[t]
			step.pvx = vax
			step.pvy = vay
			step.field = t.Field(i)
		}
		s.compareAny(vvx, vvy)
	}
}

// report records the result of a single comparison.
// It also calls Report if any reporter is registered.
func (s *state) report(eq bool, vx, vy reflect.Value) {
	s.eq = s.eq && eq
	if s.reporter != nil {
		s.reporter.Report(vx, vy, eq, s.curPath)
	}
}

// makeAddressable returns a value that is always addressable.
// It returns the input verbatim if it is already addressable,
// otherwise it creates a new value and returns an addressable copy.
func makeAddressable(v reflect.Value) reflect.Value {
	if v.CanAddr() {
		return v
	}
	vc := reflect.New(v.Type()).Elem()
	vc.Set(v)
	return vc
}

type funcType int

const (
	invalidFunc     funcType    = iota
	equalFunc                   // func(T, T) bool
	equalIfaceFunc              // func(T, I) bool
	transformFunc               // func(T) R
	valueFilterFunc = equalFunc // func(T, T) bool
)

var boolType = reflect.TypeOf(true)

// functionType identifies which type of function signature this is.
func functionType(t reflect.Type) funcType {
	if t == nil || t.Kind() != reflect.Func || t.IsVariadic() {
		return invalidFunc
	}
	ni, no := t.NumIn(), t.NumOut()
	switch {
	case ni == 2 && no == 1 && t.In(0) == t.In(1) && t.Out(0) == boolType:
		return equalFunc // or valueFilterFunc
	case ni == 2 && no == 1 && t.In(0).AssignableTo(t.In(1)) && t.Out(0) == boolType:
		return equalIfaceFunc
	case ni == 1 && no == 1:
		return transformFunc
	default:
		return invalidFunc
	}
}
