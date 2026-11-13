// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type issue324Interface interface{ Equal(issue324Interface) bool }
type issue324Value struct{}

func (issue324Value) Equal(issue324Interface) bool { return false }

type issue324EmbeddedValue struct{ issue324Value }
type issue324Pointer struct{}

func (*issue324Pointer) Equal(issue324Interface) bool { return false }

type issue324EmbeddedPointer struct{ issue324Pointer }
type issue324EmbeddedPointerField struct{ *issue324Pointer }
type issue324Override struct{ issue324Value }

func (*issue324Override) Equal(issue324Interface) bool { return true }

type issue324Panicking struct{}

func (*issue324Panicking) Equal(issue324Interface) bool { panic("user Equal panic") }

func TestIssue324(t *testing.T) {
	for _, tc := range []struct {
		name      string
		x, y      any
		wantEqual bool
		wantPanic string
	}{
		{name: "direct value receiver both nil", x: (*issue324Value)(nil), y: (*issue324Value)(nil), wantEqual: true},
		{name: "direct value receiver one nil", x: (*issue324Value)(nil), y: &issue324Value{}},
		{name: "promoted value receiver both nil", x: (*issue324EmbeddedValue)(nil), y: (*issue324EmbeddedValue)(nil), wantEqual: true},
		{name: "promoted value receiver one nil", x: (*issue324EmbeddedValue)(nil), y: &issue324EmbeddedValue{}},
		{name: "promoted value receiver nonnil", x: &issue324EmbeddedValue{}, y: &issue324EmbeddedValue{}},
		{name: "direct pointer receiver both nil", x: (*issue324Pointer)(nil), y: (*issue324Pointer)(nil)},
		{name: "direct pointer receiver one nil", x: (*issue324Pointer)(nil), y: &issue324Pointer{}},
		{name: "overriding pointer receiver both nil", x: (*issue324Override)(nil), y: (*issue324Override)(nil), wantEqual: true},
		{name: "overriding pointer receiver one nil", x: (*issue324Override)(nil), y: &issue324Override{}, wantEqual: true},
		{name: "promoted pointer receiver outer value", x: issue324EmbeddedPointer{}, y: issue324EmbeddedPointer{}, wantPanic: "cannot handle unexported field"},
		{name: "promoted pointer receiver outer pointer", x: &issue324EmbeddedPointer{}, y: &issue324EmbeddedPointer{}},
		{name: "embedded pointer field both outer nil", x: (*issue324EmbeddedPointerField)(nil), y: (*issue324EmbeddedPointerField)(nil), wantEqual: true},
		{name: "embedded pointer field one outer nil", x: (*issue324EmbeddedPointerField)(nil), y: &issue324EmbeddedPointerField{}},
		{name: "embedded pointer field inner nil", x: &issue324EmbeddedPointerField{}, y: &issue324EmbeddedPointerField{}},
		{name: "user method panic", x: (*issue324Panicking)(nil), y: (*issue324Panicking)(nil), wantPanic: "user Equal panic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("reverse=%v", reverse), func(t *testing.T) {
					x, y := tc.x, tc.y
					if reverse {
						x, y = y, x
					}
					for _, useDiff := range []bool{false, true} {
						t.Run(fmt.Sprintf("diff=%v", useDiff), func(t *testing.T) {
							defer func() {
								p := recover()
								if p == nil && tc.wantPanic != "" {
									t.Errorf("missing panic %q", tc.wantPanic)
								} else if p != nil && (tc.wantPanic == "" || !strings.Contains(fmt.Sprint(p), tc.wantPanic)) {
									t.Errorf("unexpected panic: %v", p)
								}
							}()
							var got bool
							if useDiff {
								got = cmp.Diff(x, y) == ""
							} else {
								got = cmp.Equal(x, y)
							}
							if got != tc.wantEqual {
								t.Errorf("equal = %v, want %v", got, tc.wantEqual)
							}
						})
					}
				})
			}
		})
	}
}
