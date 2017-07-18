// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmp

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// Option configures for specific behavior of Equal and Diff. In particular,
// the fundamental Option functions (Ignore, Transformer, and Comparer),
// configure how equality is determined.
//
// The fundamental options may be composed with filters (FilterPath and
// FilterValues) to control the scope over which they are applied.
//
// The cmp/cmpopts package provides helper functions for creating options that
// may be used with Equal and Diff.
type Option interface {
	// Prevent Option from being equivalent to interface{}, which provides
	// a small type checking benefit by preventing Equal(opt, x, y).
	option()
}

// Options is a list of Option values that also satisfies the Option interface.
// Helper comparison packages may return an Options value when packing multiple
// Option values into a single Option. When this package processes an Options,
// it will be implicitly expanded into a flat list.
//
// Applying a filter on an Options is equivalent to applying that same filter
// on all individual options held within.
type Options []Option

func (Options) option() {}

type (
	pathFilter  func(Path) bool
	valueFilter struct {
		in  reflect.Type  // T
		fnc reflect.Value // func(T, T) bool
	}
)

type option struct {
	typeFilter   reflect.Type
	pathFilters  []pathFilter
	valueFilters []valueFilter

	// op is the operation to perform. If nil, then this acts as an ignore.
	op interface{} // nil | *transformer | *comparer
}

func (option) option() {}

func (o option) String() string {
	// TODO: Add information about the caller?
	// TODO: Maintain the order that filters were added?

	var ss []string
	switch op := o.op.(type) {
	case *transformer:
		fn := getFuncName(op.fnc.Pointer())
		ss = append(ss, fmt.Sprintf("Transformer(%s, %s)", op.name, fn))
	case *comparer:
		fn := getFuncName(op.fnc.Pointer())
		ss = append(ss, fmt.Sprintf("Comparer(%s)", fn))
	default:
		ss = append(ss, "Ignore()")
	}

	for _, f := range o.pathFilters {
		fn := getFuncName(reflect.ValueOf(f).Pointer())
		ss = append(ss, fmt.Sprintf("FilterPath(%s)", fn))
	}
	for _, f := range o.valueFilters {
		fn := getFuncName(f.fnc.Pointer())
		ss = append(ss, fmt.Sprintf("FilterValues(%s)", fn))
	}
	return strings.Join(ss, "\n\t")
}

// getFuncName returns a short function name from the pointer.
// The string parsing logic works up until Go1.9.
func getFuncName(p uintptr) string {
	fnc := runtime.FuncForPC(p)
	if fnc == nil {
		return "<unknown>"
	}
	name := fnc.Name() // E.g., "long/path/name/mypkg.(mytype).(long/path/name/mypkg.myfunc)-fm"
	if strings.HasSuffix(name, ")-fm") || strings.HasSuffix(name, ")·fm") {
		// Strip the package name from method name.
		name = strings.TrimSuffix(name, ")-fm")
		name = strings.TrimSuffix(name, ")·fm")
		if i := strings.LastIndexByte(name, '('); i >= 0 {
			methodName := name[i+1:] // E.g., "long/path/name/mypkg.myfunc"
			if j := strings.LastIndexByte(methodName, '.'); j >= 0 {
				methodName = methodName[j+1:] // E.g., "myfunc"
			}
			name = name[:i] + methodName // E.g., "long/path/name/mypkg.(mytype)." + "myfunc"
		}
	}
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		// Strip the package name.
		name = name[i+1:] // E.g., "mypkg.(mytype).myfunc"
	}
	return name
}

// FilterPath returns a new Option where opt is only evaluated if filter f
// returns true for the current Path in the value tree.
//
// The option passed in may be an Ignore, Transformer, Comparer, Options, or
// a previously filtered Option.
func FilterPath(f func(Path) bool, opt Option) Option {
	if f == nil {
		panic("invalid path filter function")
	}
	switch opt := opt.(type) {
	case Options:
		var opts []Option
		for _, o := range opt {
			opts = append(opts, FilterPath(f, o)) // Append to slice copy
		}
		return Options(opts)
	case option:
		n := len(opt.pathFilters)
		opt.pathFilters = append(opt.pathFilters[:n:n], f) // Append to copy
		return opt
	default:
		panic(fmt.Sprintf("unknown option type: %T", opt))
	}
}

// FilterValues returns a new Option where opt is only evaluated if filter f,
// which is a function of the form "func(T, T) bool", returns true for the
// current pair of values being compared. If the type of the values is not
// assignable to T, then this filter implicitly returns false.
//
// The filter function must be
// symmetric (i.e., agnostic to the order of the inputs) and
// deterministic (i.e., produces the same result when given the same inputs).
// If T is an interface, it is possible that f is called with two values with
// different concrete types that both implement T.
//
// The option passed in may be an Ignore, Transformer, Comparer, Options, or
// a previously filtered Option.
func FilterValues(f interface{}, opt Option) Option {
	v := reflect.ValueOf(f)
	if functionType(v.Type()) != valueFilterFunc || v.IsNil() {
		panic(fmt.Sprintf("invalid values filter function: %T", f))
	}
	switch opt := opt.(type) {
	case Options:
		var opts []Option
		for _, o := range opt {
			opts = append(opts, FilterValues(f, o)) // Append to slice copy
		}
		return Options(opts)
	case option:
		n := len(opt.valueFilters)
		vf := valueFilter{v.Type().In(0), v}
		opt.valueFilters = append(opt.valueFilters[:n:n], vf) // Append to copy
		return opt
	default:
		panic(fmt.Sprintf("unknown option type: %T", opt))
	}
}

// Ignore is an Option that causes all comparisons to be ignored.
// This value is intended to be combined with FilterPath or FilterValues.
// It is an error to pass an unfiltered Ignore option to Equal.
func Ignore() Option {
	return option{}
}

// Transformer returns an Option that applies a transformation function that
// converts values of a certain type into that of another.
//
// The transformer f must be a function "func(T) R" that converts values of
// type T to those of type R and is implicitly filtered to input values
// assignable to T. The transformer must not mutate T in any way.
// If T and R are the same type, an additional filter must be applied to
// act as the base case to prevent an infinite recursion applying the same
// transform to itself (see the SortedSlice example).
//
// The name is a user provided label that is used as the Transform.Name in the
// transformation PathStep. If empty, an arbitrary name is used.
func Transformer(name string, f interface{}) Option {
	v := reflect.ValueOf(f)
	if functionType(v.Type()) != transformFunc || v.IsNil() {
		panic(fmt.Sprintf("invalid transformer function: %T", f))
	}
	if name == "" {
		name = "λ" // Lambda-symbol as place-holder for anonymous transformer
	}
	if !isValid(name) {
		panic(fmt.Sprintf("invalid name: %q", name))
	}
	opt := option{op: &transformer{name, reflect.ValueOf(f)}}
	if ti := v.Type().In(0); ti.Kind() != reflect.Interface || ti.NumMethod() > 0 {
		opt.typeFilter = ti
	}
	return opt
}

type transformer struct {
	name string
	fnc  reflect.Value // func(T) R
}

// Comparer returns an Option that determines whether two values are equal
// to each other.
//
// The comparer f must be a function "func(T, T) bool" and is implicitly
// filtered to input values assignable to T. If T is an interface, it is
// possible that f is called with two values of different concrete types that
// both implement T.
//
// The equality function must be:
//	• Symmetric: equal(x, y) == equal(y, x)
//	• Deterministic: equal(x, y) == equal(x, y)
//	• Pure: equal(x, y) does not modify x or y
func Comparer(f interface{}) Option {
	v := reflect.ValueOf(f)
	if functionType(v.Type()) != equalFunc || v.IsNil() {
		panic(fmt.Sprintf("invalid comparer function: %T", f))
	}
	opt := option{op: &comparer{v}}
	if ti := v.Type().In(0); ti.Kind() != reflect.Interface || ti.NumMethod() > 0 {
		opt.typeFilter = ti
	}
	return opt
}

type comparer struct {
	fnc reflect.Value // func(T, T) bool
}

// AllowUnexported returns an Option that forcibly allows operations on
// unexported fields in certain structs, which are specified by passing in a
// value of each struct type.
//
// Users of this option must understand that comparing on unexported fields
// from external packages is not safe since changes in the internal
// implementation of some external package may cause the result of Equal
// to unexpectedly change. However, it may be valid to use this option on types
// defined in an internal package where the semantic meaning of an unexported
// field is in the control of the user.
//
// For some cases, a custom Comparer should be used instead that defines
// equality as a function of the public API of a type rather than the underlying
// unexported implementation.
//
// For example, the reflect.Type documentation defines equality to be determined
// by the == operator on the interface (essentially performing a shallow pointer
// comparison) and most attempts to compare *regexp.Regexp types are interested
// in only checking that the regular expression strings are equal.
// Both of these are accomplished using Comparers:
//
//	Comparer(func(x, y reflect.Type) bool { return x == y })
//	Comparer(func(x, y *regexp.Regexp) bool { return x.String() == y.String() })
//
// In other cases, the cmpopts.IgnoreUnexported option can be used to ignore
// all unexported fields on specified struct types.
func AllowUnexported(types ...interface{}) Option {
	if !supportAllowUnexported {
		panic("AllowUnexported is not supported on App Engine Classic or GopherJS")
	}
	m := make(map[reflect.Type]bool)
	for _, typ := range types {
		t := reflect.TypeOf(typ)
		if t.Kind() != reflect.Struct {
			panic(fmt.Sprintf("invalid struct type: %T", typ))
		}
		m[t] = true
	}
	return visibleStructs(m)
}

type visibleStructs map[reflect.Type]bool

func (visibleStructs) option() {}

// reporter is an Option that configures how differences are reported.
//
// TODO: Not exported yet, see concerns in defaultReporter.Report.
type reporter interface {
	Option

	// Report is called for every comparison made and will be provided with
	// the two values being compared, the equality result, and the
	// current path in the value tree. It is possible for x or y to be an
	// invalid reflect.Value if one of the values is non-existent;
	// which is possible with maps and slices.
	Report(x, y reflect.Value, eq bool, p Path)

	// TODO: Perhaps add PushStep and PopStep and change Report to only accept
	// a PathStep instead of the full-path? This change allows us to provide
	// better output closer to what pretty.Compare is able to achieve.
}
