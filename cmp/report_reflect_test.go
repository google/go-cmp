// Copyright 2026, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmp

import (
	"reflect"
	"testing"
	"time"
)

func TestFormatMapKeyDisambiguatesTimeLocations(t *testing.T) {
	utc := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	fixed := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.FixedZone("UTC", 0))
	if utc == fixed {
		t.Fatal("test requires time values with distinct location pointers")
	}
	if utc.String() != fixed.String() {
		t.Fatal("test requires time values with identical String output")
	}

	utcKey := formatMapKey(reflect.ValueOf(utc), true, new(pointerReferences))
	fixedKey := formatMapKey(reflect.ValueOf(fixed), true, new(pointerReferences))
	if utcKey == fixedKey {
		t.Fatalf("formatMapKey returned identical output for distinct keys: %q", utcKey)
	}
}
