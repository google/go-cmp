// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmp

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp/internal/value"
)

type defaultReporter struct {
	Option
	diffs  []string // List of differences, possibly truncated
	ndiffs int      // Total number of differences
	nbytes int      // Number of bytes in diffs
	nlines int      // Number of lines in diffs
}

var _ reporter = (*defaultReporter)(nil)

func (r *defaultReporter) Report(x, y reflect.Value, eq bool, p Path) {
	if eq {
		return // Ignore equal results
	}
	sx := value.Format(x, value.FormatConfig{UseStringer: true})
	sy := value.Format(y, value.FormatConfig{UseStringer: true})
	if sx == sy {
		// Unhelpful output, so use more exact formatting.
		sx = value.Format(x, value.FormatConfig{PrintPrimitiveType: true})
		sy = value.Format(y, value.FormatConfig{PrintPrimitiveType: true})
	}
	// Tab delimted output in the format of: Key,Value x,Value X,
	s := fmt.Sprintf("%#v\t%s\t%s\t\n", p, sx, sy)
	r.diffs = append(r.diffs, s)
	r.nbytes += len(s)
	r.nlines += strings.Count(s, "\n")
}

func (r *defaultReporter) String() string {
	s := strings.Join(r.diffs, "")
	return s
}
