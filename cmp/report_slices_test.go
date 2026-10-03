// Copyright 2019, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmp

import (
	"reflect"
	"testing"
)

// TestCleanupSurroundingIdenticalEdgeGroups verifies that identical spans
// detected at the very start or very end of the group list are retained.
//
// When the first (or last) group is an unequal group that has leading (or
// trailing) identical elements, there is no adjacent group to fold the span
// into, so a new group must be prepended (or appended). That mutation must
// happen after the iteration completes, since inserting into the slice
// mid-loop would invalidate the group indexes. Previously the code used a
// deferred closure to mutate the local slice variable, but the function has
// an unnamed result, so the return value was already assigned before the
// deferred closures ran. The mutation was therefore silently discarded and
// those identical elements were dropped from the report.
func TestCleanupSurroundingIdenticalEdgeGroups(t *testing.T) {
	tests := []struct {
		name   string
		groups []diffStats
		eq     func(i, j int) bool
		want   []diffStats
	}{
		{
			name:   "LeadingIdenticalWithNoPrecedingGroup",
			groups: []diffStats{{Name: "byte", NumRemoved: 2, NumInserted: 1}},
			// Leading span: eq(0,0) is true. Trailing: eq(1,0) is false.
			eq:   func(i, j int) bool { return i == 0 },
			want: []diffStats{{Name: "byte", NumIdentical: 1}, {Name: "byte", NumRemoved: 1}},
		},
		{
			name:   "TrailingIdenticalWithNoSucceedingGroup",
			groups: []diffStats{{Name: "byte", NumRemoved: 1, NumInserted: 2}},
			// Leading: eq(0,0) is false. Trailing span: eq(0,1) is true.
			eq:   func(i, j int) bool { return j == 1 },
			want: []diffStats{{Name: "byte", NumInserted: 1}, {Name: "byte", NumIdentical: 1}},
		},
		{
			// x span is [A C B], y span is [A B]: one leading identical and
			// one trailing identical element, with no adjacent group for either.
			name:   "BothEdgesOfASingleUnequalGroup",
			groups: []diffStats{{Name: "byte", NumRemoved: 3, NumInserted: 2}},
			eq:     func(i, j int) bool { return (i == 0 && j == 0) || (i == 2 && j == 1) },
			want: []diffStats{
				{Name: "byte", NumIdentical: 1},
				{Name: "byte", NumRemoved: 1},
				{Name: "byte", NumIdentical: 1},
			},
		},
		{
			// A middle group with a leading identical span still folds into its
			// preceding equal group; behaviour here must not change.
			name: "MiddleGroupKeepsFoldingIntoNeighbours",
			groups: []diffStats{
				{Name: "byte", NumIdentical: 4},
				{Name: "byte", NumRemoved: 2, NumInserted: 1},
				{Name: "byte", NumIdentical: 3},
			},
			eq: func(i, j int) bool { return i == 4 && j == 4 },
			want: []diffStats{
				{Name: "byte", NumIdentical: 5},
				{Name: "byte", NumRemoved: 1},
				{Name: "byte", NumIdentical: 3},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanupSurroundingIdentical(tt.groups, tt.eq)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cleanupSurroundingIdentical(%v, eq) = %v, want %v", tt.groups, got, tt.want)
			}
		})
	}
}
