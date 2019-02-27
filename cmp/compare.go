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
// by using an Ignore option (see cmpopts.IgnoreUnexported) or explicitly compared
// using the AllowUnexported option.
package cmp

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp/internal/diff"
	"github.com/google/go-cmp/cmp/internal/flags"
	"github.com/google/go-cmp/cmp/internal/function"
	"github.com/google/go-cmp/cmp/internal/value"
)

// Equal reports whether x and y are equal by recursively applying the
// following rules in the given order to x and y and all of their sub-values:
//
// • Let S be the set of all Ignore, Transformer, and Comparer options that
// remain after applying all path filters, value filters, and type filters.
// If at least one Ignore exists in S, then the comparison is ignored.
// If the number of Transformer and Comparer options in S is greater than one,
// then Equal panics because it is ambiguous which option to use.
// If S contains a single Transformer, then use that to transform the current
// values and recursively call Equal on the output values.
// If S contains a single Comparer, then use that to compare the current values.
// Otherwise, evaluation proceeds to the next rule.
//
// • If the values have an Equal method of the form "(T) Equal(T) bool" or
// "(T) Equal(I) bool" where T is assignable to I, then use the result of
// x.Equal(y) even if x or y is nil. Otherwise, no such method exists and
// evaluation proceeds to the next rule.
//
// • Lastly, try to compare x and y based on their basic kinds.
// Simple kinds like booleans, integers, floats, complex numbers, strings, and
// channels are compared using the equivalent of the == operator in Go.
// Functions are only equal if they are both nil, otherwise they are unequal.
//
// Structs are equal if recursively calling Equal on all fields report equal.
// If a struct contains unexported fields, Equal panics unless an Ignore option
// (e.g., cmpopts.IgnoreUnexported) ignores that field or the AllowUnexported
// option explicitly permits comparing the unexported field.
//
// Slices are equal if they are both nil or both non-nil, where recursively
// calling Equal on all non-ignored slice or array elements report equal.
// Empty non-nil slices and nil slices are not equal; to equate empty slices,
// consider using cmpopts.EquateEmpty.
//
// Maps are equal if they are both nil or both non-nil, where recursively
// calling Equal on all non-ignored map entries report equal.
// Map keys are equal according to the == operator.
// To use custom comparisons for map keys, consider using cmpopts.SortMaps.
// Empty non-nil maps and nil maps are not equal; to equate empty maps,
// consider using cmpopts.EquateEmpty.
//
// Pointers and interfaces are equal if they are both nil or both non-nil,
// where they have the same underlying concrete type and recursively
// calling Equal on the underlying values reports equal.
func Equal(x, y interface{}, opts ...Option) bool {
	vx := reflect.ValueOf(x)
	vy := reflect.ValueOf(y)

	// If the inputs are different types, auto-wrap them in an empty interface
	// so that they have the same parent type.
	var t reflect.Type
	if !vx.IsValid() || !vy.IsValid() || vx.Type() != vy.Type() {
		t = reflect.TypeOf((*interface{})(nil)).Elem()
		if vx.IsValid() {
			vvx := reflect.New(t).Elem()
			vvx.Set(vx)
			vx = vvx
		}
		if vy.IsValid() {
			vvy := reflect.New(t).Elem()
			vvy.Set(vy)
			vy = vvy
		}
	} else {
		t = vx.Type()
	}

	s := newState(opts)
	s.compareAny(&pathStep{t, vx, vy})
	return s.result.Equal()
}

// Diff returns a human-readable report of the differences between two values.
// It returns an empty string if and only if Equal returns true for the same
// input values and options.
//
// The output is displayed as a literal in pseudo-Go syntax.
// At the start of each line, a "-" prefix indicates an element removed from x,
// a "+" prefix to indicates an element added to y, and the lack of a prefix
// indicates an element common to both x and y. If possible, the output
// uses fmt.Stringer.String or error.Error methods to produce more humanly
// readable outputs. In such cases, the string is prefixed with either an
// 's' or 'e' character, respectively, to indicate that the method was called.
//
// Do not depend on this output being stable.
func Diff(x, y interface{}, opts ...Option) string {
	r := new(defaultReporter)
	opts = Options{Options(opts), reporter(r)}
	eq := Equal(x, y, opts...)
	d := r.String()
	if (d == "") != eq {
		panic("inconsistent difference and equality results")
	}
	return d
}

type state struct {
	// These fields represent the "comparison state".
	// Calling statelessCompare must not result in observable changes to these.
	result    diff.Result      // The current result of comparison
	curPath   Path             // The current path in the value tree
	reporters []reporterOption // Optional reporters

	// recChecker checks for infinite cycles applying the same set of
	// transformers upon the output of itself.
	recChecker recChecker

	// dynChecker triggers pseudo-random checks for option correctness.
	// It is safe for statelessCompare to mutate this value.
	dynChecker dynChecker

	// These fields, once set by processOption, will not change.
	exporters map[reflect.Type]bool // Set of structs with unexported field visibility
	opts      Options               // List of all fundamental and filter options
}

func newState(opts []Option) *state {
	// Always ensure a validator option exists to validate the inputs.
	s := &state{opts: Options{validator{}}}
	for _, opt := range opts {
		s.processOption(opt)
	}
	return s
}

func (s *state) processOption(opt Option) {
	switch opt := opt.(type) {
	case nil:
	case Options:
		for _, o := range opt {
			s.processOption(o)
		}
	case coreOption:
		type filtered interface {
			isFiltered() bool
		}
		if fopt, ok := opt.(filtered); ok && !fopt.isFiltered() {
			panic(fmt.Sprintf("cannot use an unfiltered option: %v", opt))
		}
		s.opts = append(s.opts, opt)
	case visibleStructs:
		if s.exporters == nil {
			s.exporters = make(map[reflect.Type]bool)
		}
		for t := range opt {
			s.exporters[t] = true
		}
	case reporterOption:
		s.reporters = append(s.reporters, opt)
	default:
		panic(fmt.Sprintf("unknown option %T", opt))
	}
}

// statelessCompare compares two values and returns the result.
// This function is stateless in that it does not alter the current result,
// or output to any registered reporters.
func (s *state) statelessCompare(step PathStep) diff.Result {
	// We do not save and restore the curPath because all of the compareX
	// methods should properly push and pop from the path.
	// It is an implementation bug if the contents of curPath differs from
	// when calling this function to when returning from it.

	oldResult, oldReporters := s.result, s.reporters
	s.result = diff.Result{} // Reset result
	s.reporters = nil        // Remove reporters to avoid spurious printouts
	s.compareAny(step)
	res := s.result
	s.result, s.reporters = oldResult, oldReporters
	return res
}

func (s *state) compareAny(step PathStep) {
	// TODO: Support cyclic data structures.

	// Update the path stack.
	s.curPath.push(step)
	defer s.curPath.pop()
	for _, r := range s.reporters {
		r.PushStep(step)
		defer r.PopStep()
	}
	s.recChecker.Check(s.curPath)

	// Obtain the current type and values.
	t := step.Type()
	vx, vy := step.Values()

	// Rule 1: Check whether an option applies on this node in the value tree.
	if s.tryOptions(t, vx, vy) {
		return
	}

	// Rule 2: Check whether the type has a valid Equal method.
	if s.tryMethod(t, vx, vy) {
		return
	}

	// Rule 3: Recursively descend into each value's underlying kind.
	switch t.Kind() {
	case reflect.Bool:
		s.report(vx.Bool() == vy.Bool(), 0)
		return
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s.report(vx.Int() == vy.Int(), 0)
		return
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		s.report(vx.Uint() == vy.Uint(), 0)
		return
	case reflect.Float32, reflect.Float64:
		s.report(vx.Float() == vy.Float(), 0)
		return
	case reflect.Complex64, reflect.Complex128:
		s.report(vx.Complex() == vy.Complex(), 0)
		return
	case reflect.String:
		s.report(vx.String() == vy.String(), 0)
		return
	case reflect.Chan, reflect.UnsafePointer:
		s.report(vx.Pointer() == vy.Pointer(), 0)
		return
	case reflect.Func:
		s.report(vx.IsNil() && vy.IsNil(), 0)
		return
	case reflect.Struct:
		s.compareStruct(t, vx, vy)
		return
	case reflect.Slice:
		if vx.IsNil() || vy.IsNil() {
			s.report(vx.IsNil() && vy.IsNil(), 0)
			return
		}
		fallthrough
	case reflect.Array:
		s.compareSlice(t, vx, vy)
		return
	case reflect.Map:
		s.compareMap(t, vx, vy)
		return
	case reflect.Ptr:
		if vx.IsNil() || vy.IsNil() {
			s.report(vx.IsNil() && vy.IsNil(), 0)
			return
		}
		vx, vy = vx.Elem(), vy.Elem()
		s.compareAny(&indirect{pathStep{t.Elem(), vx, vy}})
		return
	case reflect.Interface:
		if vx.IsNil() || vy.IsNil() {
			s.report(vx.IsNil() && vy.IsNil(), 0)
			return
		}
		vx, vy = vx.Elem(), vy.Elem()
		if vx.Type() != vy.Type() {
			s.report(false, 0)
			return
		}
		s.compareAny(&typeAssertion{pathStep{vx.Type(), vx, vy}})
		return
	default:
		panic(fmt.Sprintf("%v kind not handled", t.Kind()))
	}
}

func (s *state) tryOptions(t reflect.Type, vx, vy reflect.Value) bool {
	// Evaluate all filters and apply the remaining options.
	if opt := s.opts.filter(s, t, vx, vy); opt != nil {
		opt.apply(s, vx, vy)
		return true
	}
	return false
}

func (s *state) tryMethod(t reflect.Type, vx, vy reflect.Value) bool {
	// Check if this type even has an Equal method.
	m, ok := t.MethodByName("Equal")
	if !ok || !function.IsType(m.Type, function.EqualAssignable) {
		return false
	}

	eq := s.callTTBFunc(m.Func, vx, vy)
	s.report(eq, reportByMethod)
	return true
}

func (s *state) callTRFunc(f, v reflect.Value, step *transform) reflect.Value {
	v = sanitizeValue(v, f.Type().In(0))
	if !s.dynChecker.Next() {
		return f.Call([]reflect.Value{v})[0]
	}

	// Run the function twice and ensure that we get the same results back.
	// We run in goroutines so that the race detector (if enabled) can detect
	// unsafe mutations to the input.
	c := make(chan reflect.Value)
	go detectRaces(c, f, v)
	got := <-c
	want := f.Call([]reflect.Value{v})[0]
	if step.vx, step.vy = got, want; !s.statelessCompare(step).Equal() {
		// To avoid false-positives with non-reflexive equality operations,
		// we sanity check whether a value is equal to itself.
		if step.vx, step.vy = want, want; !s.statelessCompare(step).Equal() {
			return want
		}
		panic(fmt.Sprintf("non-deterministic function detected: %s", function.NameOf(f)))
	}
	return want
}

func (s *state) callTTBFunc(f, x, y reflect.Value) bool {
	x = sanitizeValue(x, f.Type().In(0))
	y = sanitizeValue(y, f.Type().In(1))
	if !s.dynChecker.Next() {
		return f.Call([]reflect.Value{x, y})[0].Bool()
	}

	// Swapping the input arguments is sufficient to check that
	// f is symmetric and deterministic.
	// We run in goroutines so that the race detector (if enabled) can detect
	// unsafe mutations to the input.
	c := make(chan reflect.Value)
	go detectRaces(c, f, y, x)
	got := <-c
	want := f.Call([]reflect.Value{x, y})[0].Bool()
	if !got.IsValid() || got.Bool() != want {
		panic(fmt.Sprintf("non-deterministic or non-symmetric function detected: %s", function.NameOf(f)))
	}
	return want
}

func detectRaces(c chan<- reflect.Value, f reflect.Value, vs ...reflect.Value) {
	var ret reflect.Value
	defer func() {
		recover() // Ignore panics, let the other call to f panic instead
		c <- ret
	}()
	ret = f.Call(vs)[0]
}

// sanitizeValue converts nil interfaces of type T to those of type R,
// assuming that T is assignable to R.
// Otherwise, it returns the input value as is.
func sanitizeValue(v reflect.Value, t reflect.Type) reflect.Value {
	// TODO(dsnet): Workaround for reflect bug (https://golang.org/issue/22143).
	if !flags.AtLeastGo110 {
		if v.Kind() == reflect.Interface && v.IsNil() && v.Type() != t {
			return reflect.New(t).Elem()
		}
	}
	return v
}

func (s *state) compareStruct(t reflect.Type, vx, vy reflect.Value) {
	var vax, vay reflect.Value // Addressable versions of vx and vy

	step := &structField{}
	for i := 0; i < t.NumField(); i++ {
		step.typ = t.Field(i).Type
		step.vx = vx.Field(i)
		step.vy = vy.Field(i)
		step.name = t.Field(i).Name
		step.idx = i
		step.unexported = !isExported(step.name)
		if step.unexported {
			if step.name == "_" {
				continue
			}
			// Defer checking of unexported fields until later to give an
			// Ignore a chance to ignore the field.
			if !vax.IsValid() || !vay.IsValid() {
				// For retrieveUnexportedField to work, the parent struct must
				// be addressable. Create a new copy of the values if
				// necessary to make them addressable.
				vax = makeAddressable(vx)
				vay = makeAddressable(vy)
			}
			step.mayForce = s.exporters[t]
			step.pvx = vax
			step.pvy = vay
			step.field = t.Field(i)
		}
		s.compareAny(step)
	}
}

func (s *state) compareSlice(t reflect.Type, vx, vy reflect.Value) {
	step := &sliceIndex{pathStep: pathStep{typ: t.Elem()}}
	withIndexes := func(ix, iy int) *sliceIndex {
		if ix >= 0 {
			step.vx, step.xkey = vx.Index(ix), ix
		} else {
			step.vx, step.xkey = reflect.Value{}, -1
		}
		if iy >= 0 {
			step.vy, step.ykey = vy.Index(iy), iy
		} else {
			step.vy, step.ykey = reflect.Value{}, -1
		}
		return step
	}

	// Ignore options are able to ignore missing elements in a slice.
	// However, detecting these reliably requires an optimal differencing
	// algorithm, for which diff.Difference is not.
	//
	// Instead, we first iterate through both slices to detect which elements
	// would be ignored if standing alone. The index of non-discarded elements
	// are stored in a separate slice, which diffing is then performed on.
	var indexesX, indexesY []int
	var ignoredX, ignoredY []bool
	for ix := 0; ix < vx.Len(); ix++ {
		ignored := s.statelessCompare(withIndexes(ix, -1)).NumDiff == 0
		if !ignored {
			indexesX = append(indexesX, ix)
		}
		ignoredX = append(ignoredX, ignored)
	}
	for iy := 0; iy < vy.Len(); iy++ {
		ignored := s.statelessCompare(withIndexes(-1, iy)).NumDiff == 0
		if !ignored {
			indexesY = append(indexesY, iy)
		}
		ignoredY = append(ignoredY, ignored)
	}

	// Compute an edit-script for slices vx and vy (excluding ignored elements).
	edits := diff.Difference(len(indexesX), len(indexesY), func(ix, iy int) diff.Result {
		return s.statelessCompare(withIndexes(indexesX[ix], indexesY[iy]))
	})

	// Replay the ignore-scripts and the edit-script.
	var ix, iy int
	for ix < vx.Len() || iy < vy.Len() {
		var e diff.EditType
		switch {
		case ix < len(ignoredX) && ignoredX[ix]:
			e = diff.UniqueX
		case iy < len(ignoredY) && ignoredY[iy]:
			e = diff.UniqueY
		default:
			e, edits = edits[0], edits[1:]
		}
		switch e {
		case diff.UniqueX:
			s.compareAny(withIndexes(ix, -1))
			ix++
		case diff.UniqueY:
			s.compareAny(withIndexes(-1, iy))
			iy++
		default:
			s.compareAny(withIndexes(ix, iy))
			ix++
			iy++
		}
	}
	return
}

func (s *state) compareMap(t reflect.Type, vx, vy reflect.Value) {
	if vx.IsNil() || vy.IsNil() {
		s.report(vx.IsNil() && vy.IsNil(), 0)
		return
	}

	// We combine and sort the two map keys so that we can perform the
	// comparisons in a deterministic order.
	step := &mapIndex{pathStep: pathStep{typ: t.Elem()}}
	for _, k := range value.SortKeys(append(vx.MapKeys(), vy.MapKeys()...)) {
		step.vx = vx.MapIndex(k)
		step.vy = vy.MapIndex(k)
		step.key = k
		if !step.vx.IsValid() && !step.vy.IsValid() {
			// It is possible for both vx and vy to be invalid if the
			// key contained a NaN value in it.
			//
			// Even with the ability to retrieve NaN keys in Go 1.12,
			// there still isn't a sensible way to compare the values since
			// a NaN key may map to multiple unordered values.
			// The most reasonable way to compare NaNs would be to compare the
			// set of values. However, this is impossible to do efficiently
			// since set equality is provably an O(n^2) operation given only
			// an Equal function. If we had a Less function or Hash function,
			// this could be done in O(n*log(n)) or O(n), respectively.
			//
			// Rather than adding complex logic to deal with NaNs, make it
			// the user's responsibility to compare such obscure maps.
			const help = "consider providing a Comparer to compare the map"
			panic(fmt.Sprintf("%#v has map key with NaNs\n%s", s.curPath, help))
		}
		s.compareAny(step)
	}
}

func (s *state) report(eq bool, rf reportFlags) {
	if rf&reportIgnored == 0 {
		if eq {
			s.result.NumSame++
			rf |= reportEqual
		} else {
			s.result.NumDiff++
			rf |= reportUnequal
		}
	}
	for _, r := range s.reporters {
		r.Report(rf)
	}
}

// recChecker tracks the state needed to periodically perform checks that
// user provided transformers are not stuck in an infinitely recursive cycle.
type recChecker struct{ next int }

// Check scans the Path for any recursive transformers and panics when any
// recursive transformers are detected. Note that the presence of a
// recursive Transformer does not necessarily imply an infinite cycle.
// As such, this check only activates after some minimal number of path steps.
func (rc *recChecker) Check(p Path) {
	const minLen = 1 << 16
	if rc.next == 0 {
		rc.next = minLen
	}
	if len(p) < rc.next {
		return
	}
	rc.next <<= 1

	// Check whether the same transformer has appeared at least twice.
	var ss []string
	m := map[Option]int{}
	for _, ps := range p {
		if t, ok := ps.(Transform); ok {
			t := t.Option()
			if m[t] == 1 { // Transformer was used exactly once before
				tf := t.(*transformer).fnc.Type()
				ss = append(ss, fmt.Sprintf("%v: %v => %v", t, tf.In(0), tf.Out(0)))
			}
			m[t]++
		}
	}
	if len(ss) > 0 {
		const warning = "recursive set of Transformers detected"
		const help = "consider using cmpopts.AcyclicTransformer"
		set := strings.Join(ss, "\n\t")
		panic(fmt.Sprintf("%s:\n\t%s\n%s", warning, set, help))
	}
}

// dynChecker tracks the state needed to periodically perform checks that
// user provided functions are symmetric and deterministic.
// The zero value is safe for immediate use.
type dynChecker struct{ curr, next int }

// Next increments the state and reports whether a check should be performed.
//
// Checks occur every Nth function call, where N is a triangular number:
//	0 1 3 6 10 15 21 28 36 45 55 66 78 91 105 120 136 153 171 190 ...
// See https://en.wikipedia.org/wiki/Triangular_number
//
// This sequence ensures that the cost of checks drops significantly as
// the number of functions calls grows larger.
func (dc *dynChecker) Next() bool {
	ok := dc.curr == dc.next
	if ok {
		dc.curr = 0
		dc.next++
	}
	dc.curr++
	return ok
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
