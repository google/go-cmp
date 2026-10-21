// Copyright 2026, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmpopts_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestIgnoreMapEntries_stringKeys(t *testing.T) {
	first := map[string]int{
		"a": 1,
		"b": 2,
		"c": 3,
	}
	second := map[string]int{
		"a": 1,
		"b": 3,
		"c": 5,
	}

	if diff := cmp.Diff(first, second, cmpopts.IgnoreMapKeys("b", "c")); diff != "" {
		t.Errorf("Unexpected difference:\n%s", diff)
	}
}

func TestIgnoreMapEntries_intKeys(t *testing.T) {
	first := map[int]int{
		1: 10,
		2: 20,
		3: 30,
	}
	second := map[int]int{
		1: 10,
		2: 30,
		3: 50,
	}

	if diff := cmp.Diff(first, second, cmpopts.IgnoreMapKeys(2, 3)); diff != "" {
		t.Errorf("Unexpected difference:\n%s", diff)
	}
}
