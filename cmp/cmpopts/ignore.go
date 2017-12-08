// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmpopts

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/internal/value"
)

// IgnoreFields returns an Option that ignores fields of the given names
// on a single struct type (and any immediate nested or embedded structs).
// The struct type is specified by passing in a value of that type.
//
// The name may be a dot-delimited string (e.g., "Foo.Bar") to ignore a
// specific sub-field that is embedded or nested within the parent struct.
func IgnoreFields(typ interface{}, names ...string) cmp.Option {
	sf := newStructFilter(typ, names...)
	return cmp.FilterPath(sf.filter, cmp.Ignore())
}

// IgnoreUnsetFields return an Option that ignores struct fields
// based on which fields (or sub-fields from nested or embedded structs) have
// a zero value in the template struct.
//
// The fields of template must not contain self-referencing pointers.
func IgnoreUnsetFields(template interface{}) cmp.Option {
	v := reflect.ValueOf(template)
	if value.IsZero(v) {
		// This occurs if someone thinks IgnoreUnsetFields only ignores
		// the unset fields of the two values being compared and that template
		// is only provided to give type information.
		panic("all fields in template are unset; consider using IgnoreTypes instead")
	}
	names := collectUnsetFields(v)
	return IgnoreFields(template, names...)
}

// collectUnsetFields returns a slice of canonical names of unset fields.
func collectUnsetFields(v reflect.Value, path ...string) (unset []string) {
	if v.Kind() != reflect.Struct {
		return unset
	}
	for i := 0; i < v.NumField(); i++ {
		path = append(path, v.Type().Field(i).Name)
		vf := v.Field(i)
		if value.IsZero(vf) {
			unset = append(unset, strings.Join(path, "."))
		} else {
			if vf.Kind() == reflect.Ptr {
				vf = vf.Elem()
			}
			unset = append(unset, collectUnsetFields(vf, path...)...)
		}
		path = path[:len(path)-1]
	}
	return unset
}

// IgnoreTypes returns an Option that ignores all values assignable to
// certain types, which are specified by passing in a value of each type.
func IgnoreTypes(typs ...interface{}) cmp.Option {
	tf := newTypeFilter(typs...)
	return cmp.FilterPath(tf.filter, cmp.Ignore())
}

type typeFilter []reflect.Type

func newTypeFilter(typs ...interface{}) (tf typeFilter) {
	for _, typ := range typs {
		t := reflect.TypeOf(typ)
		if t == nil {
			// This occurs if someone tries to pass in sync.Locker(nil)
			panic("cannot determine type; consider using IgnoreInterfaces")
		}
		tf = append(tf, t)
	}
	return tf
}
func (tf typeFilter) filter(p cmp.Path) bool {
	if len(p) < 1 {
		return false
	}
	t := p.Last().Type()
	for _, ti := range tf {
		if t.AssignableTo(ti) {
			return true
		}
	}
	return false
}

// IgnoreInterfaces returns an Option that ignores all values or references of
// values assignable to certain interface types. These interfaces are specified
// by passing in an anonymous struct with the interface types embedded in it.
// For example, to ignore sync.Locker, pass in struct{sync.Locker}{}.
func IgnoreInterfaces(ifaces interface{}) cmp.Option {
	tf := newIfaceFilter(ifaces)
	return cmp.FilterPath(tf.filter, cmp.Ignore())
}

type ifaceFilter []reflect.Type

func newIfaceFilter(ifaces interface{}) (tf ifaceFilter) {
	t := reflect.TypeOf(ifaces)
	if ifaces == nil || t.Name() != "" || t.Kind() != reflect.Struct {
		panic("input must be an anonymous struct")
	}
	for i := 0; i < t.NumField(); i++ {
		fi := t.Field(i)
		switch {
		case !fi.Anonymous:
			panic("struct cannot have named fields")
		case fi.Type.Kind() != reflect.Interface:
			panic("embedded field must be an interface type")
		case fi.Type.NumMethod() == 0:
			// This matches everything; why would you ever want this?
			panic("cannot ignore empty interface")
		default:
			tf = append(tf, fi.Type)
		}
	}
	return tf
}
func (tf ifaceFilter) filter(p cmp.Path) bool {
	if len(p) < 1 {
		return false
	}
	t := p.Last().Type()
	for _, ti := range tf {
		if t.AssignableTo(ti) {
			return true
		}
		if t.Kind() != reflect.Ptr && reflect.PtrTo(t).AssignableTo(ti) {
			return true
		}
	}
	return false
}

// IgnoreUnexported returns an Option that only ignores the immediate unexported
// fields of a struct, including anonymous fields of unexported types.
// In particular, unexported fields within the struct's exported fields
// of struct types, including anonymous fields, will not be ignored unless the
// type of the field itself is also passed to IgnoreUnexported.
func IgnoreUnexported(typs ...interface{}) cmp.Option {
	ux := newUnexportedFilter(typs...)
	return cmp.FilterPath(ux.filter, cmp.Ignore())
}

type unexportedFilter struct{ m map[reflect.Type]bool }

func newUnexportedFilter(typs ...interface{}) unexportedFilter {
	ux := unexportedFilter{m: make(map[reflect.Type]bool)}
	for _, typ := range typs {
		t := reflect.TypeOf(typ)
		if t == nil || t.Kind() != reflect.Struct {
			panic(fmt.Sprintf("invalid struct type: %T", typ))
		}
		ux.m[t] = true
	}
	return ux
}
func (xf unexportedFilter) filter(p cmp.Path) bool {
	sf, ok := p.Index(-1).(cmp.StructField)
	if !ok {
		return false
	}
	return xf.m[p.Index(-2).Type()] && !isExported(sf.Name())
}

// isExported reports whether the identifier is exported.
func isExported(id string) bool {
	r, _ := utf8.DecodeRuneInString(id)
	return unicode.IsUpper(r)
}
