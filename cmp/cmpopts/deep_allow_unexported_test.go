// Copyright 2018, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmpopts

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDeepAllowUnexported(t *testing.T) {
	type unexported1 struct {
		a int
	}
	type unexported2 struct {
		a int
		b unexported1
	}
	type unexportedRecursive struct {
		a unexported2
		b *unexportedRecursive
	}

	u1 := unexported1{a: 1}
	u2 := unexported2{a: 2, b: u1}
	u3 := unexportedRecursive{
		a: u2,
		b: &unexportedRecursive{
			a: u2,
		},
	}

	t.Run("raw", func(t *testing.T) {
		t.Run("unexported", func(t *testing.T) {
			v := u1
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported nested", func(t *testing.T) {
			v := u2
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported recursive", func(t *testing.T) {
			v := u3
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
	})
	t.Run("pointer", func(t *testing.T) {
		t.Run("unexported", func(t *testing.T) {
			v := &u1
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported nested", func(t *testing.T) {
			v := &u2
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported recursive", func(t *testing.T) {
			v := &u3
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
	})
	t.Run("interface", func(t *testing.T) {
		type iface interface{}
		t.Run("unexported", func(t *testing.T) {
			v := iface(u1)
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported nested", func(t *testing.T) {
			v := iface(u2)
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported recursive", func(t *testing.T) {
			v := iface(u3)
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
	})
	t.Run("slice", func(t *testing.T) {
		t.Run("unexported", func(t *testing.T) {
			v := []unexported1{u1}
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported nested", func(t *testing.T) {
			v := []unexported2{u2}
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported recursive", func(t *testing.T) {
			v := []unexportedRecursive{u3}
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
	})
	t.Run("map value", func(t *testing.T) {
		t.Run("unexported", func(t *testing.T) {
			v := map[string]unexported1{"have": u1}
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported nested", func(t *testing.T) {
			v := map[string]unexported2{"have": u2}
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
		t.Run("unexported recursive", func(t *testing.T) {
			v := map[string]unexportedRecursive{"have": u3}
			if ok := cmp.Equal(v, v, DeepAllowUnexported(v, v)); !ok {
				t.Errorf("expected types to be equal but saw diff:\n%s", cmp.Diff(v, v, DeepAllowUnexported(v, v)))
			}
		})
	})
}
