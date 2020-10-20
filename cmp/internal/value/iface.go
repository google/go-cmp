// Copyright 2020, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package value

import "reflect"

var emptyIfaceType = reflect.TypeOf((*interface{})(nil)).Elem()

// IsEmptyInterface reports whether t is an interface type with no methods.
func IsEmptyInterface(t reflect.Type) bool {
	return t.Kind() == reflect.Interface && emptyIfaceType.Implements(t)
}
