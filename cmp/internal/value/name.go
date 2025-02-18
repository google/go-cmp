// Copyright 2020, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package value

import (
	"reflect"
	"strconv"
)

var anyType = reflect.TypeOf((*interface{})(nil)).Elem()

// TypeString is nearly identical to reflect.Type.String,
// but has an additional option to specify that full type names be used.
func TypeString(t reflect.Type, qualified bool) string {
	interfaceName := interfaceNameNotQualified
	if qualified {
		interfaceName = interfaceNameQualified
	}

	return string(appendTypeName(nil, t, interfaceName, qualified, false))
}

func TypeStringNotQualified(t reflect.Type) string {
	return string(appendTypeName(nil, t, interfaceNameNotQualified, false, false))
}

func appendTypeName(b []byte, t reflect.Type, interfaceName interfaceNamer, qualified, elideFunc bool) []byte {
	// BUG: Go reflection provides no way to disambiguate two named types
	// of the same name and within the same package,
	// but declared within the namespace of different functions.

	// Use the "any" alias instead of "interface{}" for better readability.
	if t == anyType {
		return append(b, "any"...)
	}

	// Named type.
	if t.Name() != "" {
		if qualified && t.PkgPath() != "" {
			b = append(b, '"')
			b = append(b, t.PkgPath()...)
			b = append(b, '"')
			b = append(b, '.')
			b = append(b, t.Name()...)
		} else {
			b = append(b, t.String()...)
		}
		return b
	}

	// Unnamed type.
	switch k := t.Kind(); k {
	case reflect.Bool, reflect.String, reflect.UnsafePointer,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		b = append(b, k.String()...)
	case reflect.Chan:
		if t.ChanDir() == reflect.RecvDir {
			b = append(b, "<-"...)
		}
		b = append(b, "chan"...)
		if t.ChanDir() == reflect.SendDir {
			b = append(b, "<-"...)
		}
		b = append(b, ' ')
		b = appendTypeName(b, t.Elem(), interfaceName, qualified, false)
	case reflect.Func:
		if !elideFunc {
			b = append(b, "func"...)
		}
		b = append(b, '(')
		for i := 0; i < t.NumIn(); i++ {
			if i > 0 {
				b = append(b, ", "...)
			}
			if i == t.NumIn()-1 && t.IsVariadic() {
				b = append(b, "..."...)
				b = appendTypeName(b, t.In(i).Elem(), interfaceName, qualified, false)
			} else {
				b = appendTypeName(b, t.In(i), interfaceName, qualified, false)
			}
		}
		b = append(b, ')')
		switch t.NumOut() {
		case 0:
			// Do nothing
		case 1:
			b = append(b, ' ')
			b = appendTypeName(b, t.Out(0), interfaceName, qualified, false)
		default:
			b = append(b, " ("...)
			for i := 0; i < t.NumOut(); i++ {
				if i > 0 {
					b = append(b, ", "...)
				}
				b = appendTypeName(b, t.Out(i), interfaceName, qualified, false)
			}
			b = append(b, ')')
		}
	case reflect.Struct:
		b = append(b, "struct{ "...)
		for i := 0; i < t.NumField(); i++ {
			if i > 0 {
				b = append(b, "; "...)
			}
			sf := t.Field(i)
			if !sf.Anonymous {
				if qualified && sf.PkgPath != "" {
					b = append(b, '"')
					b = append(b, sf.PkgPath...)
					b = append(b, '"')
					b = append(b, '.')
				}
				b = append(b, sf.Name...)
				b = append(b, ' ')
			}
			b = appendTypeName(b, sf.Type, interfaceName, qualified, false)
			if sf.Tag != "" {
				b = append(b, ' ')
				b = strconv.AppendQuote(b, string(sf.Tag))
			}
		}
		if b[len(b)-1] == ' ' {
			b = b[:len(b)-1]
		} else {
			b = append(b, ' ')
		}
		b = append(b, '}')
	case reflect.Slice, reflect.Array:
		b = append(b, '[')
		if k == reflect.Array {
			b = strconv.AppendUint(b, uint64(t.Len()), 10)
		}
		b = append(b, ']')
		b = appendTypeName(b, t.Elem(), interfaceName, qualified, false)
	case reflect.Map:
		b = append(b, "map["...)
		b = appendTypeName(b, t.Key(), interfaceName, qualified, false)
		b = append(b, ']')
		b = appendTypeName(b, t.Elem(), interfaceName, qualified, false)
	case reflect.Ptr:
		b = append(b, '*')
		b = appendTypeName(b, t.Elem(), interfaceName, qualified, false)
	case reflect.Interface:
		b = interfaceName(b, t)
	default:
		panic("invalid kind: " + k.String())
	}
	return b
}

type interfaceNamer func([]byte, reflect.Type) []byte

func interfaceNameQualified(b []byte, t reflect.Type) []byte {
	b = append(b, "interface { "...)
	for i := 0; i < t.NumMethod(); i++ {
		if i > 0 {
			b = append(b, "; "...)
		}
		m := t.Method(i)
		if m.PkgPath != "" {
			b = append(b, '"')
			b = append(b, m.PkgPath...)
			b = append(b, '"')
			b = append(b, '.')
		}
		b = append(b, m.Name...)
		b = appendTypeName(b, m.Type, interfaceNameQualified, true, true)
	}
	if b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	} else {
		b = append(b, ' ')
	}
	b = append(b, '}')
	return b
}

func interfaceNameNotQualified(b []byte, t reflect.Type) []byte {
	return append(b, t.String()...)
}
