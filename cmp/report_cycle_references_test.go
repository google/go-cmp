// Copyright 2026, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmp_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDiffCrossSideCycleReferences(t *testing.T) {
	type node struct {
		Parent *node
		Child  *node
	}
	child := &node{}
	left := &node{Child: child}
	right := &node{Child: child}
	child.Parent = right
	for _, test := range []struct {
		name string
		x, y *node
	}{
		{"forward", left, right},
		{"reverse", right, left},
	} {
		t.Run(test.name, func(t *testing.T) {
			if cmp.Equal(test.x, test.y) {
				t.Fatal("different cycle topology compares equal")
			}
			diff := cmp.Diff(test.x, test.y)
			var removed, inserted []string
			for _, line := range strings.Split(diff, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "-") {
					removed = append(removed, strings.Join(strings.Fields(line[1:]), " "))
				} else if strings.HasPrefix(line, "+") {
					inserted = append(inserted, strings.Join(strings.Fields(line[1:]), " "))
				}
			}
			if len(removed) == 0 || len(inserted) == 0 {
				t.Fatalf("missing difference: %s", diff)
			}
			if strings.Join(removed, "\n") == strings.Join(inserted, "\n") {
				t.Fatalf("different cycle topology has indistinguishable removed and inserted values: %s", diff)
			}
		})
	}
	t.Run("equal cycles", func(t *testing.T) {
		a, b := &node{}, &node{}
		a.Child, b.Child = a, b
		if diff := cmp.Diff(a, b); diff != "" {
			t.Fatalf("structurally equal cycles unexpectedly differ: %s", diff)
		}
	})
}
