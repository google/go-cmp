// Copyright 2017, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.md file.

package cmp_test

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"math/rand"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/go-cmp/cmp/internal/flags"

	pb "github.com/google/go-cmp/cmp/internal/testprotos"
	ts "github.com/google/go-cmp/cmp/internal/teststructs"
)

func init() {
	flags.Deterministic = true
}

var update = flag.Bool("update", false, "update golden test files")

const goldenHeaderPrefix = "<<< "
const goldenFooterPrefix = ">>> "

/// mustParseGolden parses a file as a set of key-value pairs.
//
// The syntax is simple and looks something like:
//
//	<<< Key1
//	value1a
//	value1b
//	>>> Key1
//	<<< Key2
//	value2
//	>>> Key2
//
// It is the user's responsibility to choose a sufficiently unique key name
// such that it never appears in the body of the value itself.
func mustParseGolden(path string) map[string]string {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	s := string(b)

	out := map[string]string{}
	for len(s) > 0 {
		// Identify the next header.
		i := strings.Index(s, "\n") + len("\n")
		header := s[:i]
		if !strings.HasPrefix(header, goldenHeaderPrefix) {
			panic(fmt.Sprintf("invalid header: %q", header))
		}

		// Locate the next footer.
		footer := goldenFooterPrefix + header[len(goldenHeaderPrefix):]
		j := strings.Index(s, footer)
		if j < 0 {
			panic(fmt.Sprintf("missing footer: %q", footer))
		}

		// Store the name and data.
		name := header[len(goldenHeaderPrefix) : len(header)-len("\n")]
		if _, ok := out[name]; ok {
			panic(fmt.Sprintf("duplicate name: %q", name))
		}
		out[name] = s[len(header):j]
		s = s[j+len(footer):]
	}
	return out
}
func mustFormatGolden(path string, in []struct{ Name, Data string }) {
	var b []byte
	for _, v := range in {
		b = append(b, goldenHeaderPrefix+v.Name+"\n"...)
		b = append(b, v.Data...)
		b = append(b, goldenFooterPrefix+v.Name+"\n"...)
	}
	if err := ioutil.WriteFile(path, b, 0664); err != nil {
		panic(err)
	}
}

var now = time.Date(2009, time.November, 10, 23, 00, 00, 00, time.UTC)

func intPtr(n int) *int { return &n }

type test struct {
	label     string       // Test name
	x, y      interface{}  // Input values to compare
	opts      []cmp.Option // Input options
	wantEqual bool         // Whether any difference is expected
	wantPanic string       // Sub-string of an expected panic message
	reason    string       // The reason for the expected outcome
}

func TestDiff(t *testing.T) {
	var tests []test
	tests = append(tests, comparerTests()...)
	tests = append(tests, transformerTests()...)
	tests = append(tests, reporterTests()...)
	tests = append(tests, embeddedTests()...)
	tests = append(tests, methodTests()...)
	tests = append(tests, cycleTests()...)
	tests = append(tests, project1Tests()...)
	tests = append(tests, project2Tests()...)
	tests = append(tests, project3Tests()...)
	tests = append(tests, project4Tests()...)

	const goldenFile = "testdata/diffs"
	gotDiffs := []struct{ Name, Data string }{}
	wantDiffs := mustParseGolden(goldenFile)
	for _, tt := range tests {
		tt := tt
		t.Run(tt.label, func(t *testing.T) {
			if !*update {
				t.Parallel()
			}
			var gotDiff, gotPanic string
			func() {
				defer func() {
					if ex := recover(); ex != nil {
						if s, ok := ex.(string); ok {
							gotPanic = s
						} else {
							panic(ex)
						}
					}
				}()
				gotDiff = cmp.Diff(tt.x, tt.y, tt.opts...)
			}()

			// TODO: Require every test case to provide a reason.
			if tt.wantPanic == "" {
				if gotPanic != "" {
					t.Fatalf("unexpected panic message: %s\nreason: %v", gotPanic, tt.reason)
				}
				if *update {
					if gotDiff != "" {
						gotDiffs = append(gotDiffs, struct{ Name, Data string }{t.Name(), gotDiff})
					}
				} else {
					wantDiff := wantDiffs[t.Name()]
					if gotDiff != wantDiff {
						t.Fatalf("Diff:\ngot:\n%s\nwant:\n%s\nreason: %v", gotDiff, wantDiff, tt.reason)
					}
				}
				gotEqual := gotDiff == ""
				if gotEqual != tt.wantEqual {
					t.Fatalf("Equal = %v, want %v\nreason: %v", gotEqual, tt.wantEqual, tt.reason)
				}
			} else {
				if !strings.Contains(gotPanic, tt.wantPanic) {
					t.Fatalf("panic message:\ngot:  %s\nwant: %s\nreason: %v", gotPanic, tt.wantPanic, tt.reason)
				}
			}
		})
	}

	if *update {
		mustFormatGolden(goldenFile, gotDiffs)
	}
}

func comparerTests() []test {
	const label = "Comparer"

	type Iface1 interface {
		Method()
	}
	type Iface2 interface {
		Method()
	}

	type tarHeader struct {
		Name       string
		Mode       int64
		Uid        int
		Gid        int
		Size       int64
		ModTime    time.Time
		Typeflag   byte
		Linkname   string
		Uname      string
		Gname      string
		Devmajor   int64
		Devminor   int64
		AccessTime time.Time
		ChangeTime time.Time
		Xattrs     map[string]string
	}

	type namedWithUnexported struct {
		unexported string
	}

	makeTarHeaders := func(tf byte) (hs []tarHeader) {
		for i := 0; i < 5; i++ {
			hs = append(hs, tarHeader{
				Name: fmt.Sprintf("some/dummy/test/file%d", i),
				Mode: 0664, Uid: i * 1000, Gid: i * 1000, Size: 1 << uint(i),
				ModTime: now.Add(time.Duration(i) * time.Hour),
				Uname:   "user", Gname: "group",
				Typeflag: tf,
			})
		}
		return hs
	}

	return []test{{
		label:     label,
		x:         nil,
		y:         nil,
		wantEqual: true,
	}, {
		label:     label,
		x:         1,
		y:         1,
		wantEqual: true,
	}, {
		label:     label,
		x:         1,
		y:         1,
		opts:      []cmp.Option{cmp.Ignore()},
		wantPanic: "cannot use an unfiltered option",
	}, {
		label:     label,
		x:         1,
		y:         1,
		opts:      []cmp.Option{cmp.Comparer(func(_, _ interface{}) bool { return true })},
		wantPanic: "cannot use an unfiltered option",
	}, {
		label:     label,
		x:         1,
		y:         1,
		opts:      []cmp.Option{cmp.Transformer("λ", func(x interface{}) interface{} { return x })},
		wantPanic: "cannot use an unfiltered option",
	}, {
		label: label,
		x:     1,
		y:     1,
		opts: []cmp.Option{
			cmp.Comparer(func(x, y int) bool { return true }),
			cmp.Transformer("λ", func(x int) float64 { return float64(x) }),
		},
		wantPanic: "ambiguous set of applicable options",
	}, {
		label: label,
		x:     1,
		y:     1,
		opts: []cmp.Option{
			cmp.FilterPath(func(p cmp.Path) bool {
				return len(p) > 0 && p[len(p)-1].Type().Kind() == reflect.Int
			}, cmp.Options{cmp.Ignore(), cmp.Ignore(), cmp.Ignore()}),
			cmp.Comparer(func(x, y int) bool { return true }),
			cmp.Transformer("λ", func(x int) float64 { return float64(x) }),
		},
		wantEqual: true,
	}, {
		label:     label,
		opts:      []cmp.Option{struct{ cmp.Option }{}},
		wantPanic: "unknown option",
	}, {
		label:     label,
		x:         struct{ A, B, C int }{1, 2, 3},
		y:         struct{ A, B, C int }{1, 2, 3},
		wantEqual: true,
	}, {
		label:     label,
		x:         struct{ A, B, C int }{1, 2, 3},
		y:         struct{ A, B, C int }{1, 2, 4},
		wantEqual: false,
	}, {
		label:     label,
		x:         struct{ a, b, c int }{1, 2, 3},
		y:         struct{ a, b, c int }{1, 2, 4},
		wantPanic: "cannot handle unexported field",
	}, {
		label:     label,
		x:         &struct{ A *int }{intPtr(4)},
		y:         &struct{ A *int }{intPtr(4)},
		wantEqual: true,
	}, {
		label:     label,
		x:         &struct{ A *int }{intPtr(4)},
		y:         &struct{ A *int }{intPtr(5)},
		wantEqual: false,
	}, {
		label: label,
		x:     &struct{ A *int }{intPtr(4)},
		y:     &struct{ A *int }{intPtr(5)},
		opts: []cmp.Option{
			cmp.Comparer(func(x, y int) bool { return true }),
		},
		wantEqual: true,
	}, {
		label: label,
		x:     &struct{ A *int }{intPtr(4)},
		y:     &struct{ A *int }{intPtr(5)},
		opts: []cmp.Option{
			cmp.Comparer(func(x, y *int) bool { return x != nil && y != nil }),
		},
		wantEqual: true,
	}, {
		label:     label,
		x:         &struct{ R *bytes.Buffer }{},
		y:         &struct{ R *bytes.Buffer }{},
		wantEqual: true,
	}, {
		label:     label,
		x:         &struct{ R *bytes.Buffer }{new(bytes.Buffer)},
		y:         &struct{ R *bytes.Buffer }{},
		wantEqual: false,
	}, {
		label: label,
		x:     &struct{ R *bytes.Buffer }{new(bytes.Buffer)},
		y:     &struct{ R *bytes.Buffer }{},
		opts: []cmp.Option{
			cmp.Comparer(func(x, y io.Reader) bool { return true }),
		},
		wantEqual: true,
	}, {
		label:     label,
		x:         &struct{ R bytes.Buffer }{},
		y:         &struct{ R bytes.Buffer }{},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label,
		x:     &struct{ R bytes.Buffer }{},
		y:     &struct{ R bytes.Buffer }{},
		opts: []cmp.Option{
			cmp.Comparer(func(x, y io.Reader) bool { return true }),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label,
		x:     &struct{ R bytes.Buffer }{},
		y:     &struct{ R bytes.Buffer }{},
		opts: []cmp.Option{
			cmp.Transformer("Ref", func(x bytes.Buffer) *bytes.Buffer { return &x }),
			cmp.Comparer(func(x, y io.Reader) bool { return true }),
		},
		wantEqual: true,
	}, {
		label:     label,
		x:         []*regexp.Regexp{nil, regexp.MustCompile("a*b*c*")},
		y:         []*regexp.Regexp{nil, regexp.MustCompile("a*b*c*")},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label,
		x:     []*regexp.Regexp{nil, regexp.MustCompile("a*b*c*")},
		y:     []*regexp.Regexp{nil, regexp.MustCompile("a*b*c*")},
		opts: []cmp.Option{cmp.Comparer(func(x, y *regexp.Regexp) bool {
			if x == nil || y == nil {
				return x == nil && y == nil
			}
			return x.String() == y.String()
		})},
		wantEqual: true,
	}, {
		label: label,
		x:     []*regexp.Regexp{nil, regexp.MustCompile("a*b*c*")},
		y:     []*regexp.Regexp{nil, regexp.MustCompile("a*b*d*")},
		opts: []cmp.Option{cmp.Comparer(func(x, y *regexp.Regexp) bool {
			if x == nil || y == nil {
				return x == nil && y == nil
			}
			return x.String() == y.String()
		})},
		wantEqual: false,
	}, {
		label: label,
		x: func() ***int {
			a := 0
			b := &a
			c := &b
			return &c
		}(),
		y: func() ***int {
			a := 0
			b := &a
			c := &b
			return &c
		}(),
		wantEqual: true,
	}, {
		label: label,
		x: func() ***int {
			a := 0
			b := &a
			c := &b
			return &c
		}(),
		y: func() ***int {
			a := 1
			b := &a
			c := &b
			return &c
		}(),
		wantEqual: false,
	}, {
		label:     label,
		x:         []int{1, 2, 3, 4, 5}[:3],
		y:         []int{1, 2, 3},
		wantEqual: true,
	}, {
		label:     label,
		x:         struct{ fmt.Stringer }{bytes.NewBufferString("hello")},
		y:         struct{ fmt.Stringer }{regexp.MustCompile("hello")},
		opts:      []cmp.Option{cmp.Comparer(func(x, y fmt.Stringer) bool { return x.String() == y.String() })},
		wantEqual: true,
	}, {
		label:     label,
		x:         struct{ fmt.Stringer }{bytes.NewBufferString("hello")},
		y:         struct{ fmt.Stringer }{regexp.MustCompile("hello2")},
		opts:      []cmp.Option{cmp.Comparer(func(x, y fmt.Stringer) bool { return x.String() == y.String() })},
		wantEqual: false,
	}, {
		label:     label,
		x:         md5.Sum([]byte{'a'}),
		y:         md5.Sum([]byte{'b'}),
		wantEqual: false,
	}, {
		label:     label,
		x:         new(fmt.Stringer),
		y:         nil,
		wantEqual: false,
	}, {
		label:     label,
		x:         makeTarHeaders('0'),
		y:         makeTarHeaders('\x00'),
		wantEqual: false,
	}, {
		label: label,
		x:     make([]int, 1000),
		y:     make([]int, 1000),
		opts: []cmp.Option{
			cmp.Comparer(func(_, _ int) bool {
				return rand.Intn(2) == 0
			}),
		},
		wantPanic: "non-deterministic or non-symmetric function detected",
	}, {
		label: label,
		x:     make([]int, 1000),
		y:     make([]int, 1000),
		opts: []cmp.Option{
			cmp.FilterValues(func(_, _ int) bool {
				return rand.Intn(2) == 0
			}, cmp.Ignore()),
		},
		wantPanic: "non-deterministic or non-symmetric function detected",
	}, {
		label: label,
		x:     []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		y:     []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
		opts: []cmp.Option{
			cmp.Comparer(func(x, y int) bool {
				return x < y
			}),
		},
		wantPanic: "non-deterministic or non-symmetric function detected",
	}, {
		label: label,
		x:     make([]string, 1000),
		y:     make([]string, 1000),
		opts: []cmp.Option{
			cmp.Transformer("λ", func(x string) int {
				return rand.Int()
			}),
		},
		wantPanic: "non-deterministic function detected",
	}, {
		// Make sure the dynamic checks don't raise a false positive for
		// non-reflexive comparisons.
		label: label,
		x:     make([]int, 10),
		y:     make([]int, 10),
		opts: []cmp.Option{
			cmp.Transformer("λ", func(x int) float64 {
				return math.NaN()
			}),
		},
		wantEqual: false,
	}, {
		// Ensure reasonable Stringer formatting of map keys.
		label:     label,
		x:         map[*pb.Stringer]*pb.Stringer{{"hello"}: {"world"}},
		y:         map[*pb.Stringer]*pb.Stringer(nil),
		wantEqual: false,
	}, {
		// Ensure Stringer avoids double-quote escaping if possible.
		label:     label,
		x:         []*pb.Stringer{{`multi\nline\nline\nline`}},
		wantEqual: false,
	}, {
		label: label,
		x:     struct{ I Iface2 }{},
		y:     struct{ I Iface2 }{},
		opts: []cmp.Option{
			cmp.Comparer(func(x, y Iface1) bool {
				return x == nil && y == nil
			}),
		},
		wantEqual: true,
	}, {
		label: label,
		x:     struct{ I Iface2 }{},
		y:     struct{ I Iface2 }{},
		opts: []cmp.Option{
			cmp.Transformer("λ", func(v Iface1) bool {
				return v == nil
			}),
		},
		wantEqual: true,
	}, {
		label: label,
		x:     struct{ I Iface2 }{},
		y:     struct{ I Iface2 }{},
		opts: []cmp.Option{
			cmp.FilterValues(func(x, y Iface1) bool {
				return x == nil && y == nil
			}, cmp.Ignore()),
		},
		wantEqual: true,
	}, {
		label:     label,
		x:         []interface{}{map[string]interface{}{"avg": 0.278, "hr": 65, "name": "Mark McGwire"}, map[string]interface{}{"avg": 0.288, "hr": 63, "name": "Sammy Sosa"}},
		y:         []interface{}{map[string]interface{}{"avg": 0.278, "hr": 65.0, "name": "Mark McGwire"}, map[string]interface{}{"avg": 0.288, "hr": 63.0, "name": "Sammy Sosa"}},
		wantEqual: false,
	}, {
		label: label,
		x: map[*int]string{
			new(int): "hello",
		},
		y: map[*int]string{
			new(int): "world",
		},
		wantEqual: false,
	}, {
		label: label,
		x:     intPtr(0),
		y:     intPtr(0),
		opts: []cmp.Option{
			cmp.Comparer(func(x, y *int) bool { return x == y }),
		},
		// TODO: This diff output is unhelpful and should show the address.
		wantEqual: false,
	}, {
		label: label,
		x: [2][]int{
			{0, 0, 0, 1, 2, 3, 0, 0, 4, 5, 6, 7, 8, 0, 9, 0, 0},
			{0, 1, 0, 0, 0, 20},
		},
		y: [2][]int{
			{1, 2, 3, 0, 4, 5, 6, 7, 0, 8, 9, 0, 0, 0},
			{0, 0, 1, 2, 0, 0, 0},
		},
		opts: []cmp.Option{
			cmp.FilterPath(func(p cmp.Path) bool {
				vx, vy := p.Last().Values()
				if vx.IsValid() && vx.Kind() == reflect.Int && vx.Int() == 0 {
					return true
				}
				if vy.IsValid() && vy.Kind() == reflect.Int && vy.Int() == 0 {
					return true
				}
				return false
			}, cmp.Ignore()),
		},
		wantEqual: false,
		reason:    "all zero slice elements are ignored (even if missing)",
	}, {
		label: label,
		x: [2]map[string]int{
			{"ignore1": 0, "ignore2": 0, "keep1": 1, "keep2": 2, "KEEP3": 3, "IGNORE3": 0},
			{"keep1": 1, "ignore1": 0},
		},
		y: [2]map[string]int{
			{"ignore1": 0, "ignore3": 0, "ignore4": 0, "keep1": 1, "keep2": 2, "KEEP3": 3},
			{"keep1": 1, "keep2": 2, "ignore2": 0},
		},
		opts: []cmp.Option{
			cmp.FilterPath(func(p cmp.Path) bool {
				vx, vy := p.Last().Values()
				if vx.IsValid() && vx.Kind() == reflect.Int && vx.Int() == 0 {
					return true
				}
				if vy.IsValid() && vy.Kind() == reflect.Int && vy.Int() == 0 {
					return true
				}
				return false
			}, cmp.Ignore()),
		},
		wantEqual: false,
		reason:    "all zero map entries are ignored (even if missing)",
	}, {
		label:     label,
		x:         namedWithUnexported{},
		y:         namedWithUnexported{},
		wantPanic: strconv.Quote(reflect.TypeOf(namedWithUnexported{}).PkgPath()) + ".namedWithUnexported",
		reason:    "panic on named struct type with unexported field",
	}, {
		label:     label,
		x:         struct{ a int }{},
		y:         struct{ a int }{},
		wantPanic: strconv.Quote(reflect.TypeOf(namedWithUnexported{}).PkgPath()) + ".(struct { a int })",
		reason:    "panic on unnamed struct type with unexported field",
	}}
}

func transformerTests() []test {
	type StringBytes struct {
		String string
		Bytes  []byte
	}

	const label = "Transformer"

	transformOnce := func(name string, f interface{}) cmp.Option {
		xform := cmp.Transformer(name, f)
		return cmp.FilterPath(func(p cmp.Path) bool {
			for _, ps := range p {
				if tr, ok := ps.(cmp.Transform); ok && tr.Option() == xform {
					return false
				}
			}
			return true
		}, xform)
	}

	return []test{{
		label: label,
		x:     uint8(0),
		y:     uint8(1),
		opts: []cmp.Option{
			cmp.Transformer("λ", func(in uint8) uint16 { return uint16(in) }),
			cmp.Transformer("λ", func(in uint16) uint32 { return uint32(in) }),
			cmp.Transformer("λ", func(in uint32) uint64 { return uint64(in) }),
		},
		wantEqual: false,
	}, {
		label: label,
		x:     0,
		y:     1,
		opts: []cmp.Option{
			cmp.Transformer("λ", func(in int) int { return in / 2 }),
			cmp.Transformer("λ", func(in int) int { return in }),
		},
		wantPanic: "ambiguous set of applicable options",
	}, {
		label: label,
		x:     []int{0, -5, 0, -1},
		y:     []int{1, 3, 0, -5},
		opts: []cmp.Option{
			cmp.FilterValues(
				func(x, y int) bool { return x+y >= 0 },
				cmp.Transformer("λ", func(in int) int64 { return int64(in / 2) }),
			),
			cmp.FilterValues(
				func(x, y int) bool { return x+y < 0 },
				cmp.Transformer("λ", func(in int) int64 { return int64(in) }),
			),
		},
		wantEqual: false,
	}, {
		label: label,
		x:     0,
		y:     1,
		opts: []cmp.Option{
			cmp.Transformer("λ", func(in int) interface{} {
				if in == 0 {
					return "zero"
				}
				return float64(in)
			}),
		},
		wantEqual: false,
	}, {
		label: label,
		x: `{
		  "firstName": "John",
		  "lastName": "Smith",
		  "age": 25,
		  "isAlive": true,
		  "address": {
		    "city": "Los Angeles",
		    "postalCode": "10021-3100",
		    "state": "CA",
		    "streetAddress": "21 2nd Street"
		  },
		  "phoneNumbers": [{
		    "type": "home",
		    "number": "212 555-4321"
		  },{
		    "type": "office",
		    "number": "646 555-4567"
		  },{
		    "number": "123 456-7890",
		    "type": "mobile"
		  }],
		  "children": []
		}`,
		y: `{"firstName":"John","lastName":"Smith","isAlive":true,"age":25,
			"address":{"streetAddress":"21 2nd Street","city":"New York",
			"state":"NY","postalCode":"10021-3100"},"phoneNumbers":[{"type":"home",
			"number":"212 555-1234"},{"type":"office","number":"646 555-4567"},{
			"type":"mobile","number":"123 456-7890"}],"children":[],"spouse":null}`,
		opts: []cmp.Option{
			transformOnce("ParseJSON", func(s string) (m map[string]interface{}) {
				if err := json.Unmarshal([]byte(s), &m); err != nil {
					panic(err)
				}
				return m
			}),
		},
		wantEqual: false,
	}, {
		label: label,
		x:     StringBytes{String: "some\nmulti\nLine\nstring", Bytes: []byte("some\nmulti\nline\nbytes")},
		y:     StringBytes{String: "some\nmulti\nline\nstring", Bytes: []byte("some\nmulti\nline\nBytes")},
		opts: []cmp.Option{
			transformOnce("SplitString", func(s string) []string { return strings.Split(s, "\n") }),
			transformOnce("SplitBytes", func(b []byte) [][]byte { return bytes.Split(b, []byte("\n")) }),
		},
		wantEqual: false,
	}, {
		x: "a\nb\nc\n",
		y: "a\nb\nc\n",
		opts: []cmp.Option{
			cmp.Transformer("SplitLines", func(s string) []string { return strings.Split(s, "\n") }),
		},
		wantPanic: "recursive set of Transformers detected",
	}, {
		x: complex64(0),
		y: complex64(0),
		opts: []cmp.Option{
			cmp.Transformer("T1", func(x complex64) complex128 { return complex128(x) }),
			cmp.Transformer("T2", func(x complex128) [2]float64 { return [2]float64{real(x), imag(x)} }),
			cmp.Transformer("T3", func(x float64) complex64 { return complex64(complex(x, 0)) }),
		},
		wantPanic: "recursive set of Transformers detected",
	}}
}

func reporterTests() []test {
	const label = "Reporter"

	type (
		MyString    string
		MyByte      byte
		MyBytes     []byte
		MyInt       int8
		MyInts      []int8
		MyUint      int16
		MyUints     []int16
		MyFloat     float32
		MyFloats    []float32
		MyComposite struct {
			StringA string
			StringB MyString
			BytesA  []byte
			BytesB  []MyByte
			BytesC  MyBytes
			IntsA   []int8
			IntsB   []MyInt
			IntsC   MyInts
			UintsA  []uint16
			UintsB  []MyUint
			UintsC  MyUints
			FloatsA []float32
			FloatsB []MyFloat
			FloatsC MyFloats
		}
	)

	return []test{{
		label:     label,
		x:         MyComposite{IntsA: []int8{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29}},
		y:         MyComposite{IntsA: []int8{10, 11, 21, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29}},
		wantEqual: false,
		reason:    "unbatched diffing desired since few elements differ",
	}, {
		label:     label,
		x:         MyComposite{IntsA: []int8{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29}},
		y:         MyComposite{IntsA: []int8{12, 29, 13, 27, 22, 23, 17, 18, 19, 20, 21, 10, 26, 16, 25, 28, 11, 15, 24, 14}},
		wantEqual: false,
		reason:    "batched diffing desired since many elements differ",
	}, {
		label: label,
		x: MyComposite{
			BytesA:  []byte{1, 2, 3},
			BytesB:  []MyByte{4, 5, 6},
			BytesC:  MyBytes{7, 8, 9},
			IntsA:   []int8{-1, -2, -3},
			IntsB:   []MyInt{-4, -5, -6},
			IntsC:   MyInts{-7, -8, -9},
			UintsA:  []uint16{1000, 2000, 3000},
			UintsB:  []MyUint{4000, 5000, 6000},
			UintsC:  MyUints{7000, 8000, 9000},
			FloatsA: []float32{1.5, 2.5, 3.5},
			FloatsB: []MyFloat{4.5, 5.5, 6.5},
			FloatsC: MyFloats{7.5, 8.5, 9.5},
		},
		y: MyComposite{
			BytesA:  []byte{3, 2, 1},
			BytesB:  []MyByte{6, 5, 4},
			BytesC:  MyBytes{9, 8, 7},
			IntsA:   []int8{-3, -2, -1},
			IntsB:   []MyInt{-6, -5, -4},
			IntsC:   MyInts{-9, -8, -7},
			UintsA:  []uint16{3000, 2000, 1000},
			UintsB:  []MyUint{6000, 5000, 4000},
			UintsC:  MyUints{9000, 8000, 7000},
			FloatsA: []float32{3.5, 2.5, 1.5},
			FloatsB: []MyFloat{6.5, 5.5, 4.5},
			FloatsC: MyFloats{9.5, 8.5, 7.5},
		},
		wantEqual: false,
		reason:    "batched diffing available for both named and unnamed slices",
	}, {
		label:     label,
		x:         MyComposite{BytesA: []byte("\xf3\x0f\x8a\xa4\xd3\x12R\t$\xbeX\x95A\xfd$fX\x8byT\xac\r\xd8qwp\x20j\\s\u007f\x8c\x17U\xc04\xcen\xf7\xaaG\xee2\x9d\xc5\xca\x1eX\xaf\x8f'\xf3\x02J\x90\xedi.p2\xb4\xab0 \xb6\xbd\\b4\x17\xb0\x00\xbbO~'G\x06\xf4.f\xfdc\xd7\x04ݷ0\xb7\xd1U~{\xf6\xb3~\x1dWi \x9e\xbc\xdf\xe1M\xa9\xef\xa2\xd2\xed\xb4Gx\xc9\xc9'\xa4\xc6\xce\xecDp]")},
		y:         MyComposite{BytesA: []byte("\xf3\x0f\x8a\xa4\xd3\x12R\t$\xbeT\xac\r\xd8qwp\x20j\\s\u007f\x8c\x17U\xc04\xcen\xf7\xaaG\xee2\x9d\xc5\xca\x1eX\xaf\x8f'\xf3\x02J\x90\xedi.p2\xb4\xab0 \xb6\xbd\\b4\x17\xb0\x00\xbbO~'G\x06\xf4.f\xfdc\xd7\x04ݷ0\xb7\xd1u-[]]\xf6\xb3haha~\x1dWI \x9e\xbc\xdf\xe1M\xa9\xef\xa2\xd2\xed\xb4Gx\xc9\xc9'\xa4\xc6\xce\xecDp]")},
		wantEqual: false,
		reason:    "binary diff in hexdump form since data is binary data",
	}, {
		label:     label,
		x:         MyComposite{StringB: MyString("readme.txt\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x000000600\x000000000\x000000000\x0000000000046\x0000000000000\x00011173\x00 0\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00ustar\x0000\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x000000000\x000000000\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")},
		y:         MyComposite{StringB: MyString("gopher.txt\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x000000600\x000000000\x000000000\x0000000000043\x0000000000000\x00011217\x00 0\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00ustar\x0000\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x000000000\x000000000\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")},
		wantEqual: false,
		reason:    "binary diff desired since string looks like binary data",
	}, {
		label:     label,
		x:         MyComposite{BytesA: []byte(`{"firstName":"John","lastName":"Smith","isAlive":true,"age":27,"address":{"streetAddress":"314 54th Avenue","city":"New York","state":"NY","postalCode":"10021-3100"},"phoneNumbers":[{"type":"home","number":"212 555-1234"},{"type":"office","number":"646 555-4567"},{"type":"mobile","number":"123 456-7890"}],"children":[],"spouse":null}`)},
		y:         MyComposite{BytesA: []byte(`{"firstName":"John","lastName":"Smith","isAlive":true,"age":27,"address":{"streetAddress":"21 2nd Street","city":"New York","state":"NY","postalCode":"10021-3100"},"phoneNumbers":[{"type":"home","number":"212 555-1234"},{"type":"office","number":"646 555-4567"},{"type":"mobile","number":"123 456-7890"}],"children":[],"spouse":null}`)},
		wantEqual: false,
		reason:    "batched textual diff desired since bytes looks like textual data",
	}, {
		label: label,
		x: MyComposite{
			StringA: strings.TrimPrefix(`
Package cmp determines equality of values.

This package is intended to be a more powerful and safer alternative to
reflect.DeepEqual for comparing whether two values are semantically equal.

The primary features of cmp are:

• When the default behavior of equality does not suit the needs of the test,
custom equality functions can override the equality operation.
For example, an equality function may report floats as equal so long as they
are within some tolerance of each other.

• Types that have an Equal method may use that method to determine equality.
This allows package authors to determine the equality operation for the types
that they define.

• If no custom equality functions are used and no Equal method is defined,
equality is determined by recursively comparing the primitive kinds on both
values, much like reflect.DeepEqual. Unlike reflect.DeepEqual, unexported
fields are not compared by default; they result in panics unless suppressed
by using an Ignore option (see cmpopts.IgnoreUnexported) or explicitly compared
using the AllowUnexported option.
`, "\n"),
		},
		y: MyComposite{
			StringA: strings.TrimPrefix(`
Package cmp determines equality of value.

This package is intended to be a more powerful and safer alternative to
reflect.DeepEqual for comparing whether two values are semantically equal.

The primary features of cmp are:

• When the default behavior of equality does not suit the needs of the test,
custom equality functions can override the equality operation.
For example, an equality function may report floats as equal so long as they
are within some tolerance of each other.

• If no custom equality functions are used and no Equal method is defined,
equality is determined by recursively comparing the primitive kinds on both
values, much like reflect.DeepEqual. Unlike reflect.DeepEqual, unexported
fields are not compared by default; they result in panics unless suppressed
by using an Ignore option (see cmpopts.IgnoreUnexported) or explicitly compared
using the AllowUnexported option.`, "\n"),
		},
		wantEqual: false,
		reason:    "batched per-line diff desired since string looks like multi-line textual data",
	}}
}

func embeddedTests() []test {
	const label = "EmbeddedStruct/"

	privateStruct := *new(ts.ParentStructA).PrivateStruct()

	createStructA := func(i int) ts.ParentStructA {
		s := ts.ParentStructA{}
		s.PrivateStruct().Public = 1 + i
		s.PrivateStruct().SetPrivate(2 + i)
		return s
	}

	createStructB := func(i int) ts.ParentStructB {
		s := ts.ParentStructB{}
		s.PublicStruct.Public = 1 + i
		s.PublicStruct.SetPrivate(2 + i)
		return s
	}

	createStructC := func(i int) ts.ParentStructC {
		s := ts.ParentStructC{}
		s.PrivateStruct().Public = 1 + i
		s.PrivateStruct().SetPrivate(2 + i)
		s.Public = 3 + i
		s.SetPrivate(4 + i)
		return s
	}

	createStructD := func(i int) ts.ParentStructD {
		s := ts.ParentStructD{}
		s.PublicStruct.Public = 1 + i
		s.PublicStruct.SetPrivate(2 + i)
		s.Public = 3 + i
		s.SetPrivate(4 + i)
		return s
	}

	createStructE := func(i int) ts.ParentStructE {
		s := ts.ParentStructE{}
		s.PrivateStruct().Public = 1 + i
		s.PrivateStruct().SetPrivate(2 + i)
		s.PublicStruct.Public = 3 + i
		s.PublicStruct.SetPrivate(4 + i)
		return s
	}

	createStructF := func(i int) ts.ParentStructF {
		s := ts.ParentStructF{}
		s.PrivateStruct().Public = 1 + i
		s.PrivateStruct().SetPrivate(2 + i)
		s.PublicStruct.Public = 3 + i
		s.PublicStruct.SetPrivate(4 + i)
		s.Public = 5 + i
		s.SetPrivate(6 + i)
		return s
	}

	createStructG := func(i int) *ts.ParentStructG {
		s := ts.NewParentStructG()
		s.PrivateStruct().Public = 1 + i
		s.PrivateStruct().SetPrivate(2 + i)
		return s
	}

	createStructH := func(i int) *ts.ParentStructH {
		s := ts.NewParentStructH()
		s.PublicStruct.Public = 1 + i
		s.PublicStruct.SetPrivate(2 + i)
		return s
	}

	createStructI := func(i int) *ts.ParentStructI {
		s := ts.NewParentStructI()
		s.PrivateStruct().Public = 1 + i
		s.PrivateStruct().SetPrivate(2 + i)
		s.PublicStruct.Public = 3 + i
		s.PublicStruct.SetPrivate(4 + i)
		return s
	}

	createStructJ := func(i int) *ts.ParentStructJ {
		s := ts.NewParentStructJ()
		s.PrivateStruct().Public = 1 + i
		s.PrivateStruct().SetPrivate(2 + i)
		s.PublicStruct.Public = 3 + i
		s.PublicStruct.SetPrivate(4 + i)
		s.Private().Public = 5 + i
		s.Private().SetPrivate(6 + i)
		s.Public.Public = 7 + i
		s.Public.SetPrivate(8 + i)
		return s
	}

	// TODO(dsnet): Workaround for reflect bug (https://golang.org/issue/21122).
	wantPanicNotGo110 := func(s string) string {
		if !flags.AtLeastGo110 {
			return ""
		}
		return s
	}

	return []test{{
		label:     label + "ParentStructA",
		x:         ts.ParentStructA{},
		y:         ts.ParentStructA{},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructA",
		x:     ts.ParentStructA{},
		y:     ts.ParentStructA{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructA{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructA",
		x:     createStructA(0),
		y:     createStructA(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructA{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructA",
		x:     createStructA(0),
		y:     createStructA(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructA{}, privateStruct),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructA",
		x:     createStructA(0),
		y:     createStructA(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructA{}, privateStruct),
		},
		wantEqual: false,
	}, {
		label: label + "ParentStructB",
		x:     ts.ParentStructB{},
		y:     ts.ParentStructB{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructB{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructB",
		x:     ts.ParentStructB{},
		y:     ts.ParentStructB{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructB{}),
			cmpopts.IgnoreUnexported(ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructB",
		x:     createStructB(0),
		y:     createStructB(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructB{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructB",
		x:     createStructB(0),
		y:     createStructB(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructB{}, ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructB",
		x:     createStructB(0),
		y:     createStructB(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructB{}, ts.PublicStruct{}),
		},
		wantEqual: false,
	}, {
		label:     label + "ParentStructC",
		x:         ts.ParentStructC{},
		y:         ts.ParentStructC{},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructC",
		x:     ts.ParentStructC{},
		y:     ts.ParentStructC{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructC{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructC",
		x:     createStructC(0),
		y:     createStructC(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructC{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructC",
		x:     createStructC(0),
		y:     createStructC(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructC{}, privateStruct),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructC",
		x:     createStructC(0),
		y:     createStructC(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructC{}, privateStruct),
		},
		wantEqual: false,
	}, {
		label: label + "ParentStructD",
		x:     ts.ParentStructD{},
		y:     ts.ParentStructD{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructD{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructD",
		x:     ts.ParentStructD{},
		y:     ts.ParentStructD{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructD{}),
			cmpopts.IgnoreUnexported(ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructD",
		x:     createStructD(0),
		y:     createStructD(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructD{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructD",
		x:     createStructD(0),
		y:     createStructD(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructD{}, ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructD",
		x:     createStructD(0),
		y:     createStructD(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructD{}, ts.PublicStruct{}),
		},
		wantEqual: false,
	}, {
		label: label + "ParentStructE",
		x:     ts.ParentStructE{},
		y:     ts.ParentStructE{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructE{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructE",
		x:     ts.ParentStructE{},
		y:     ts.ParentStructE{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructE{}),
			cmpopts.IgnoreUnexported(ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructE",
		x:     createStructE(0),
		y:     createStructE(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructE{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructE",
		x:     createStructE(0),
		y:     createStructE(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructE{}, ts.PublicStruct{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructE",
		x:     createStructE(0),
		y:     createStructE(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructE{}, ts.PublicStruct{}, privateStruct),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructE",
		x:     createStructE(0),
		y:     createStructE(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructE{}, ts.PublicStruct{}, privateStruct),
		},
		wantEqual: false,
	}, {
		label: label + "ParentStructF",
		x:     ts.ParentStructF{},
		y:     ts.ParentStructF{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructF{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructF",
		x:     ts.ParentStructF{},
		y:     ts.ParentStructF{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructF{}),
			cmpopts.IgnoreUnexported(ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructF",
		x:     createStructF(0),
		y:     createStructF(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructF{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructF",
		x:     createStructF(0),
		y:     createStructF(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructF{}, ts.PublicStruct{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructF",
		x:     createStructF(0),
		y:     createStructF(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructF{}, ts.PublicStruct{}, privateStruct),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructF",
		x:     createStructF(0),
		y:     createStructF(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructF{}, ts.PublicStruct{}, privateStruct),
		},
		wantEqual: false,
	}, {
		label:     label + "ParentStructG",
		x:         ts.ParentStructG{},
		y:         ts.ParentStructG{},
		wantPanic: wantPanicNotGo110("cannot handle unexported field"),
		wantEqual: !flags.AtLeastGo110,
	}, {
		label: label + "ParentStructG",
		x:     ts.ParentStructG{},
		y:     ts.ParentStructG{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructG{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructG",
		x:     createStructG(0),
		y:     createStructG(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructG{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructG",
		x:     createStructG(0),
		y:     createStructG(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructG{}, privateStruct),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructG",
		x:     createStructG(0),
		y:     createStructG(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructG{}, privateStruct),
		},
		wantEqual: false,
	}, {
		label:     label + "ParentStructH",
		x:         ts.ParentStructH{},
		y:         ts.ParentStructH{},
		wantEqual: true,
	}, {
		label:     label + "ParentStructH",
		x:         createStructH(0),
		y:         createStructH(0),
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructH",
		x:     ts.ParentStructH{},
		y:     ts.ParentStructH{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructH{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructH",
		x:     createStructH(0),
		y:     createStructH(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructH{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructH",
		x:     createStructH(0),
		y:     createStructH(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructH{}, ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructH",
		x:     createStructH(0),
		y:     createStructH(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructH{}, ts.PublicStruct{}),
		},
		wantEqual: false,
	}, {
		label:     label + "ParentStructI",
		x:         ts.ParentStructI{},
		y:         ts.ParentStructI{},
		wantPanic: wantPanicNotGo110("cannot handle unexported field"),
		wantEqual: !flags.AtLeastGo110,
	}, {
		label: label + "ParentStructI",
		x:     ts.ParentStructI{},
		y:     ts.ParentStructI{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructI{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructI",
		x:     createStructI(0),
		y:     createStructI(0),
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructI{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructI",
		x:     createStructI(0),
		y:     createStructI(0),
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructI{}, ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructI",
		x:     createStructI(0),
		y:     createStructI(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructI{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructI",
		x:     createStructI(0),
		y:     createStructI(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructI{}, ts.PublicStruct{}, privateStruct),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructI",
		x:     createStructI(0),
		y:     createStructI(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructI{}, ts.PublicStruct{}, privateStruct),
		},
		wantEqual: false,
	}, {
		label:     label + "ParentStructJ",
		x:         ts.ParentStructJ{},
		y:         ts.ParentStructJ{},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructJ",
		x:     ts.ParentStructJ{},
		y:     ts.ParentStructJ{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructJ{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructJ",
		x:     ts.ParentStructJ{},
		y:     ts.ParentStructJ{},
		opts: []cmp.Option{
			cmpopts.IgnoreUnexported(ts.ParentStructJ{}, ts.PublicStruct{}),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructJ",
		x:     createStructJ(0),
		y:     createStructJ(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructJ{}, ts.PublicStruct{}),
		},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label + "ParentStructJ",
		x:     createStructJ(0),
		y:     createStructJ(0),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructJ{}, ts.PublicStruct{}, privateStruct),
		},
		wantEqual: true,
	}, {
		label: label + "ParentStructJ",
		x:     createStructJ(0),
		y:     createStructJ(1),
		opts: []cmp.Option{
			cmp.AllowUnexported(ts.ParentStructJ{}, ts.PublicStruct{}, privateStruct),
		},
		wantEqual: false,
	}}
}

func methodTests() []test {
	const label = "EqualMethod/"

	// A common mistake that the Equal method is on a pointer receiver,
	// but only a non-pointer value is present in the struct.
	// A transform can be used to forcibly reference the value.
	derefTransform := cmp.FilterPath(func(p cmp.Path) bool {
		if len(p) == 0 {
			return false
		}
		t := p[len(p)-1].Type()
		if _, ok := t.MethodByName("Equal"); ok || t.Kind() == reflect.Ptr {
			return false
		}
		if m, ok := reflect.PtrTo(t).MethodByName("Equal"); ok {
			tf := m.Func.Type()
			return !tf.IsVariadic() && tf.NumIn() == 2 && tf.NumOut() == 1 &&
				tf.In(0).AssignableTo(tf.In(1)) && tf.Out(0) == reflect.TypeOf(true)
		}
		return false
	}, cmp.Transformer("Ref", func(x interface{}) interface{} {
		v := reflect.ValueOf(x)
		vp := reflect.New(v.Type())
		vp.Elem().Set(v)
		return vp.Interface()
	}))

	// For each of these types, there is an Equal method defined, which always
	// returns true, while the underlying data are fundamentally different.
	// Since the method should be called, these are expected to be equal.
	return []test{{
		label:     label + "StructA",
		x:         ts.StructA{X: "NotEqual"},
		y:         ts.StructA{X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructA",
		x:         &ts.StructA{X: "NotEqual"},
		y:         &ts.StructA{X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructB",
		x:         ts.StructB{X: "NotEqual"},
		y:         ts.StructB{X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructB",
		x:         ts.StructB{X: "NotEqual"},
		y:         ts.StructB{X: "not_equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: true,
	}, {
		label:     label + "StructB",
		x:         &ts.StructB{X: "NotEqual"},
		y:         &ts.StructB{X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructC",
		x:         ts.StructC{X: "NotEqual"},
		y:         ts.StructC{X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructC",
		x:         &ts.StructC{X: "NotEqual"},
		y:         &ts.StructC{X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructD",
		x:         ts.StructD{X: "NotEqual"},
		y:         ts.StructD{X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructD",
		x:         ts.StructD{X: "NotEqual"},
		y:         ts.StructD{X: "not_equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: true,
	}, {
		label:     label + "StructD",
		x:         &ts.StructD{X: "NotEqual"},
		y:         &ts.StructD{X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructE",
		x:         ts.StructE{X: "NotEqual"},
		y:         ts.StructE{X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructE",
		x:         ts.StructE{X: "NotEqual"},
		y:         ts.StructE{X: "not_equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: true,
	}, {
		label:     label + "StructE",
		x:         &ts.StructE{X: "NotEqual"},
		y:         &ts.StructE{X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructF",
		x:         ts.StructF{X: "NotEqual"},
		y:         ts.StructF{X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructF",
		x:         &ts.StructF{X: "NotEqual"},
		y:         &ts.StructF{X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructA1",
		x:         ts.StructA1{StructA: ts.StructA{X: "NotEqual"}, X: "equal"},
		y:         ts.StructA1{StructA: ts.StructA{X: "not_equal"}, X: "equal"},
		wantEqual: true,
	}, {
		label:     label + "StructA1",
		x:         ts.StructA1{StructA: ts.StructA{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructA1{StructA: ts.StructA{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructA1",
		x:         &ts.StructA1{StructA: ts.StructA{X: "NotEqual"}, X: "equal"},
		y:         &ts.StructA1{StructA: ts.StructA{X: "not_equal"}, X: "equal"},
		wantEqual: true,
	}, {
		label:     label + "StructA1",
		x:         &ts.StructA1{StructA: ts.StructA{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructA1{StructA: ts.StructA{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructB1",
		x:         ts.StructB1{StructB: ts.StructB{X: "NotEqual"}, X: "equal"},
		y:         ts.StructB1{StructB: ts.StructB{X: "not_equal"}, X: "equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: true,
	}, {
		label:     label + "StructB1",
		x:         ts.StructB1{StructB: ts.StructB{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructB1{StructB: ts.StructB{X: "not_equal"}, X: "not_equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: false,
	}, {
		label:     label + "StructB1",
		x:         &ts.StructB1{StructB: ts.StructB{X: "NotEqual"}, X: "equal"},
		y:         &ts.StructB1{StructB: ts.StructB{X: "not_equal"}, X: "equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: true,
	}, {
		label:     label + "StructB1",
		x:         &ts.StructB1{StructB: ts.StructB{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructB1{StructB: ts.StructB{X: "not_equal"}, X: "not_equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: false,
	}, {
		label:     label + "StructC1",
		x:         ts.StructC1{StructC: ts.StructC{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructC1{StructC: ts.StructC{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructC1",
		x:         &ts.StructC1{StructC: ts.StructC{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructC1{StructC: ts.StructC{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructD1",
		x:         ts.StructD1{StructD: ts.StructD{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructD1{StructD: ts.StructD{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructD1",
		x:         ts.StructD1{StructD: ts.StructD{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructD1{StructD: ts.StructD{X: "not_equal"}, X: "not_equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: true,
	}, {
		label:     label + "StructD1",
		x:         &ts.StructD1{StructD: ts.StructD{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructD1{StructD: ts.StructD{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructE1",
		x:         ts.StructE1{StructE: ts.StructE{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructE1{StructE: ts.StructE{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructE1",
		x:         ts.StructE1{StructE: ts.StructE{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructE1{StructE: ts.StructE{X: "not_equal"}, X: "not_equal"},
		opts:      []cmp.Option{derefTransform},
		wantEqual: true,
	}, {
		label:     label + "StructE1",
		x:         &ts.StructE1{StructE: ts.StructE{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructE1{StructE: ts.StructE{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructF1",
		x:         ts.StructF1{StructF: ts.StructF{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructF1{StructF: ts.StructF{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructF1",
		x:         &ts.StructF1{StructF: ts.StructF{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructF1{StructF: ts.StructF{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructA2",
		x:         ts.StructA2{StructA: &ts.StructA{X: "NotEqual"}, X: "equal"},
		y:         ts.StructA2{StructA: &ts.StructA{X: "not_equal"}, X: "equal"},
		wantEqual: true,
	}, {
		label:     label + "StructA2",
		x:         ts.StructA2{StructA: &ts.StructA{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructA2{StructA: &ts.StructA{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructA2",
		x:         &ts.StructA2{StructA: &ts.StructA{X: "NotEqual"}, X: "equal"},
		y:         &ts.StructA2{StructA: &ts.StructA{X: "not_equal"}, X: "equal"},
		wantEqual: true,
	}, {
		label:     label + "StructA2",
		x:         &ts.StructA2{StructA: &ts.StructA{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructA2{StructA: &ts.StructA{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructB2",
		x:         ts.StructB2{StructB: &ts.StructB{X: "NotEqual"}, X: "equal"},
		y:         ts.StructB2{StructB: &ts.StructB{X: "not_equal"}, X: "equal"},
		wantEqual: true,
	}, {
		label:     label + "StructB2",
		x:         ts.StructB2{StructB: &ts.StructB{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructB2{StructB: &ts.StructB{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructB2",
		x:         &ts.StructB2{StructB: &ts.StructB{X: "NotEqual"}, X: "equal"},
		y:         &ts.StructB2{StructB: &ts.StructB{X: "not_equal"}, X: "equal"},
		wantEqual: true,
	}, {
		label:     label + "StructB2",
		x:         &ts.StructB2{StructB: &ts.StructB{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructB2{StructB: &ts.StructB{X: "not_equal"}, X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "StructC2",
		x:         ts.StructC2{StructC: &ts.StructC{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructC2{StructC: &ts.StructC{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructC2",
		x:         &ts.StructC2{StructC: &ts.StructC{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructC2{StructC: &ts.StructC{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructD2",
		x:         ts.StructD2{StructD: &ts.StructD{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructD2{StructD: &ts.StructD{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructD2",
		x:         &ts.StructD2{StructD: &ts.StructD{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructD2{StructD: &ts.StructD{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructE2",
		x:         ts.StructE2{StructE: &ts.StructE{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructE2{StructE: &ts.StructE{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructE2",
		x:         &ts.StructE2{StructE: &ts.StructE{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructE2{StructE: &ts.StructE{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructF2",
		x:         ts.StructF2{StructF: &ts.StructF{X: "NotEqual"}, X: "NotEqual"},
		y:         ts.StructF2{StructF: &ts.StructF{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructF2",
		x:         &ts.StructF2{StructF: &ts.StructF{X: "NotEqual"}, X: "NotEqual"},
		y:         &ts.StructF2{StructF: &ts.StructF{X: "not_equal"}, X: "not_equal"},
		wantEqual: true,
	}, {
		label:     label + "StructNo",
		x:         ts.StructNo{X: "NotEqual"},
		y:         ts.StructNo{X: "not_equal"},
		wantEqual: false,
	}, {
		label:     label + "AssignA",
		x:         ts.AssignA(func() int { return 0 }),
		y:         ts.AssignA(func() int { return 1 }),
		wantEqual: true,
	}, {
		label:     label + "AssignB",
		x:         ts.AssignB(struct{ A int }{0}),
		y:         ts.AssignB(struct{ A int }{1}),
		wantEqual: true,
	}, {
		label:     label + "AssignC",
		x:         ts.AssignC(make(chan bool)),
		y:         ts.AssignC(make(chan bool)),
		wantEqual: true,
	}, {
		label:     label + "AssignD",
		x:         ts.AssignD(make(chan bool)),
		y:         ts.AssignD(make(chan bool)),
		wantEqual: true,
	}}
}

type (
	CycleAlpha struct {
		Name   string
		Bravos map[string]*CycleBravo
	}
	CycleBravo struct {
		ID     int
		Name   string
		Mods   int
		Alphas map[string]*CycleAlpha
	}
)

func cycleTests() []test {
	const label = "Cycle"

	type (
		P *P
		S []S
		M map[int]M
	)

	makeGraph := func() map[string]*CycleAlpha {
		v := map[string]*CycleAlpha{
			"Foo": &CycleAlpha{
				Name: "Foo",
				Bravos: map[string]*CycleBravo{
					"FooBravo": &CycleBravo{
						Name: "FooBravo",
						ID:   101,
						Mods: 100,
						Alphas: map[string]*CycleAlpha{
							"Foo": nil, // cyclic reference
						},
					},
				},
			},
			"Bar": &CycleAlpha{
				Name: "Bar",
				Bravos: map[string]*CycleBravo{
					"BarBuzzBravo": &CycleBravo{
						Name: "BarBuzzBravo",
						ID:   102,
						Mods: 2,
						Alphas: map[string]*CycleAlpha{
							"Bar":  nil, // cyclic reference
							"Buzz": nil, // cyclic reference
						},
					},
					"BuzzBarBravo": &CycleBravo{
						Name: "BuzzBarBravo",
						ID:   103,
						Mods: 0,
						Alphas: map[string]*CycleAlpha{
							"Bar":  nil, // cyclic reference
							"Buzz": nil, // cyclic reference
						},
					},
				},
			},
			"Buzz": &CycleAlpha{
				Name: "Buzz",
				Bravos: map[string]*CycleBravo{
					"BarBuzzBravo": nil, // cyclic reference
					"BuzzBarBravo": nil, // cyclic reference
				},
			},
		}
		v["Foo"].Bravos["FooBravo"].Alphas["Foo"] = v["Foo"]
		v["Bar"].Bravos["BarBuzzBravo"].Alphas["Bar"] = v["Bar"]
		v["Bar"].Bravos["BarBuzzBravo"].Alphas["Buzz"] = v["Buzz"]
		v["Bar"].Bravos["BuzzBarBravo"].Alphas["Bar"] = v["Bar"]
		v["Bar"].Bravos["BuzzBarBravo"].Alphas["Buzz"] = v["Buzz"]
		v["Buzz"].Bravos["BarBuzzBravo"] = v["Bar"].Bravos["BarBuzzBravo"]
		v["Buzz"].Bravos["BuzzBarBravo"] = v["Bar"].Bravos["BuzzBarBravo"]
		return v
	}

	var tests []test
	type XY struct{ x, y interface{} }
	for _, tt := range []struct {
		in        XY
		wantEqual bool
		reason    string
	}{{
		in: func() XY {
			x := new(P)
			*x = x
			y := new(P)
			*y = y
			return XY{x, y}
		}(),
		wantEqual: true,
	}, {
		in: func() XY {
			x := new(P)
			*x = x
			y1, y2 := new(P), new(P)
			*y1 = y2
			*y2 = y1
			return XY{x, y1}
		}(),
		wantEqual: false,
	}, {
		in: func() XY {
			x := S{nil}
			x[0] = x
			y := S{nil}
			y[0] = y
			return XY{x, y}
		}(),
		wantEqual: true,
	}, {
		in: func() XY {
			x := S{nil}
			x[0] = x
			y1, y2 := S{nil}, S{nil}
			y1[0] = y2
			y2[0] = y1
			return XY{x, y1}
		}(),
		wantEqual: false,
	}, {
		in: func() XY {
			x := M{0: nil}
			x[0] = x
			y := M{0: nil}
			y[0] = y
			return XY{x, y}
		}(),
		wantEqual: true,
	}, {
		in: func() XY {
			x := M{0: nil}
			x[0] = x
			y1, y2 := M{0: nil}, M{0: nil}
			y1[0] = y2
			y2[0] = y1
			return XY{x, y1}
		}(),
		wantEqual: false,
	}, {
		in:        XY{makeGraph(), makeGraph()},
		wantEqual: true,
	}, {
		in: func() XY {
			x := makeGraph()
			y := makeGraph()
			y["Foo"].Bravos["FooBravo"].ID = 0
			y["Bar"].Bravos["BarBuzzBravo"].ID = 0
			y["Bar"].Bravos["BuzzBarBravo"].ID = 0
			return XY{x, y}
		}(),
		wantEqual: false,
	}, {
		in: func() XY {
			x := makeGraph()
			y := makeGraph()
			x["Buzz"].Bravos["BuzzBarBravo"] = &CycleBravo{
				Name: "BuzzBarBravo",
				ID:   103,
			}
			return XY{x, y}
		}(),
		wantEqual: false,
	}} {
		tests = append(tests, test{
			label:     label,
			x:         tt.in.x,
			y:         tt.in.y,
			wantEqual: tt.wantEqual,
			reason:    tt.reason,
		})
	}
	return tests
}

func project1Tests() []test {
	const label = "Project1"

	ignoreUnexported := cmpopts.IgnoreUnexported(
		ts.EagleImmutable{},
		ts.DreamerImmutable{},
		ts.SlapImmutable{},
		ts.GoatImmutable{},
		ts.DonkeyImmutable{},
		ts.LoveRadius{},
		ts.SummerLove{},
		ts.SummerLoveSummary{},
	)

	createEagle := func() ts.Eagle {
		return ts.Eagle{
			Name:   "eagle",
			Hounds: []string{"buford", "tannen"},
			Desc:   "some description",
			Dreamers: []ts.Dreamer{{}, {
				Name: "dreamer2",
				Animal: []interface{}{
					ts.Goat{
						Target: "corporation",
						Immutable: &ts.GoatImmutable{
							ID:      "southbay",
							State:   (*pb.Goat_States)(intPtr(5)),
							Started: now,
						},
					},
					ts.Donkey{},
				},
				Amoeba: 53,
			}},
			Slaps: []ts.Slap{{
				Name: "slapID",
				Args: &pb.MetaData{Stringer: pb.Stringer{X: "metadata"}},
				Immutable: &ts.SlapImmutable{
					ID:       "immutableSlap",
					MildSlap: true,
					Started:  now,
					LoveRadius: &ts.LoveRadius{
						Summer: &ts.SummerLove{
							Summary: &ts.SummerLoveSummary{
								Devices:    []string{"foo", "bar", "baz"},
								ChangeType: []pb.SummerType{1, 2, 3},
							},
						},
					},
				},
			}},
			Immutable: &ts.EagleImmutable{
				ID:          "eagleID",
				Birthday:    now,
				MissingCall: (*pb.Eagle_MissingCalls)(intPtr(55)),
			},
		}
	}

	return []test{{
		label: label,
		x: ts.Eagle{Slaps: []ts.Slap{{
			Args: &pb.MetaData{Stringer: pb.Stringer{X: "metadata"}},
		}}},
		y: ts.Eagle{Slaps: []ts.Slap{{
			Args: &pb.MetaData{Stringer: pb.Stringer{X: "metadata"}},
		}}},
		wantPanic: "cannot handle unexported field",
	}, {
		label: label,
		x: ts.Eagle{Slaps: []ts.Slap{{
			Args: &pb.MetaData{Stringer: pb.Stringer{X: "metadata"}},
		}}},
		y: ts.Eagle{Slaps: []ts.Slap{{
			Args: &pb.MetaData{Stringer: pb.Stringer{X: "metadata"}},
		}}},
		opts:      []cmp.Option{cmp.Comparer(pb.Equal)},
		wantEqual: true,
	}, {
		label: label,
		x: ts.Eagle{Slaps: []ts.Slap{{}, {}, {}, {}, {
			Args: &pb.MetaData{Stringer: pb.Stringer{X: "metadata"}},
		}}},
		y: ts.Eagle{Slaps: []ts.Slap{{}, {}, {}, {}, {
			Args: &pb.MetaData{Stringer: pb.Stringer{X: "metadata2"}},
		}}},
		opts:      []cmp.Option{cmp.Comparer(pb.Equal)},
		wantEqual: false,
	}, {
		label:     label,
		x:         createEagle(),
		y:         createEagle(),
		opts:      []cmp.Option{ignoreUnexported, cmp.Comparer(pb.Equal)},
		wantEqual: true,
	}, {
		label: label,
		x: func() ts.Eagle {
			eg := createEagle()
			eg.Dreamers[1].Animal[0].(ts.Goat).Immutable.ID = "southbay2"
			eg.Dreamers[1].Animal[0].(ts.Goat).Immutable.State = (*pb.Goat_States)(intPtr(6))
			eg.Slaps[0].Immutable.MildSlap = false
			return eg
		}(),
		y: func() ts.Eagle {
			eg := createEagle()
			devs := eg.Slaps[0].Immutable.LoveRadius.Summer.Summary.Devices
			eg.Slaps[0].Immutable.LoveRadius.Summer.Summary.Devices = devs[:1]
			return eg
		}(),
		opts:      []cmp.Option{ignoreUnexported, cmp.Comparer(pb.Equal)},
		wantEqual: false,
	}}
}

type germSorter []*pb.Germ

func (gs germSorter) Len() int           { return len(gs) }
func (gs germSorter) Less(i, j int) bool { return gs[i].String() < gs[j].String() }
func (gs germSorter) Swap(i, j int)      { gs[i], gs[j] = gs[j], gs[i] }

func project2Tests() []test {
	const label = "Project2"

	sortGerms := cmp.Transformer("Sort", func(in []*pb.Germ) []*pb.Germ {
		out := append([]*pb.Germ(nil), in...) // Make copy
		sort.Sort(germSorter(out))
		return out
	})

	equalDish := cmp.Comparer(func(x, y *ts.Dish) bool {
		if x == nil || y == nil {
			return x == nil && y == nil
		}
		px, err1 := x.Proto()
		py, err2 := y.Proto()
		if err1 != nil || err2 != nil {
			return err1 == err2
		}
		return pb.Equal(px, py)
	})

	createBatch := func() ts.GermBatch {
		return ts.GermBatch{
			DirtyGerms: map[int32][]*pb.Germ{
				17: {
					{Stringer: pb.Stringer{X: "germ1"}},
				},
				18: {
					{Stringer: pb.Stringer{X: "germ2"}},
					{Stringer: pb.Stringer{X: "germ3"}},
					{Stringer: pb.Stringer{X: "germ4"}},
				},
			},
			GermMap: map[int32]*pb.Germ{
				13: {Stringer: pb.Stringer{X: "germ13"}},
				21: {Stringer: pb.Stringer{X: "germ21"}},
			},
			DishMap: map[int32]*ts.Dish{
				0: ts.CreateDish(nil, io.EOF),
				1: ts.CreateDish(nil, io.ErrUnexpectedEOF),
				2: ts.CreateDish(&pb.Dish{Stringer: pb.Stringer{X: "dish"}}, nil),
			},
			HasPreviousResult: true,
			DirtyID:           10,
			GermStrain:        421,
			InfectedAt:        now,
		}
	}

	return []test{{
		label:     label,
		x:         createBatch(),
		y:         createBatch(),
		wantPanic: "cannot handle unexported field",
	}, {
		label:     label,
		x:         createBatch(),
		y:         createBatch(),
		opts:      []cmp.Option{cmp.Comparer(pb.Equal), sortGerms, equalDish},
		wantEqual: true,
	}, {
		label: label,
		x:     createBatch(),
		y: func() ts.GermBatch {
			gb := createBatch()
			s := gb.DirtyGerms[18]
			s[0], s[1], s[2] = s[1], s[2], s[0]
			return gb
		}(),
		opts:      []cmp.Option{cmp.Comparer(pb.Equal), equalDish},
		wantEqual: false,
	}, {
		label: label,
		x:     createBatch(),
		y: func() ts.GermBatch {
			gb := createBatch()
			s := gb.DirtyGerms[18]
			s[0], s[1], s[2] = s[1], s[2], s[0]
			return gb
		}(),
		opts:      []cmp.Option{cmp.Comparer(pb.Equal), sortGerms, equalDish},
		wantEqual: true,
	}, {
		label: label,
		x: func() ts.GermBatch {
			gb := createBatch()
			delete(gb.DirtyGerms, 17)
			gb.DishMap[1] = nil
			return gb
		}(),
		y: func() ts.GermBatch {
			gb := createBatch()
			gb.DirtyGerms[18] = gb.DirtyGerms[18][:2]
			gb.GermStrain = 22
			return gb
		}(),
		opts:      []cmp.Option{cmp.Comparer(pb.Equal), sortGerms, equalDish},
		wantEqual: false,
	}}
}

func project3Tests() []test {
	const label = "Project3"

	allowVisibility := cmp.AllowUnexported(ts.Dirt{})

	ignoreLocker := cmpopts.IgnoreInterfaces(struct{ sync.Locker }{})

	transformProtos := cmp.Transformer("λ", func(x pb.Dirt) *pb.Dirt {
		return &x
	})

	equalTable := cmp.Comparer(func(x, y ts.Table) bool {
		tx, ok1 := x.(*ts.MockTable)
		ty, ok2 := y.(*ts.MockTable)
		if !ok1 || !ok2 {
			panic("table type must be MockTable")
		}
		return cmp.Equal(tx.State(), ty.State())
	})

	createDirt := func() (d ts.Dirt) {
		d.SetTable(ts.CreateMockTable([]string{"a", "b", "c"}))
		d.SetTimestamp(12345)
		d.Discord = 554
		d.Proto = pb.Dirt{Stringer: pb.Stringer{X: "proto"}}
		d.SetWizard(map[string]*pb.Wizard{
			"harry": {Stringer: pb.Stringer{X: "potter"}},
			"albus": {Stringer: pb.Stringer{X: "dumbledore"}},
		})
		d.SetLastTime(54321)
		return d
	}

	return []test{{
		label:     label,
		x:         createDirt(),
		y:         createDirt(),
		wantPanic: "cannot handle unexported field",
	}, {
		label:     label,
		x:         createDirt(),
		y:         createDirt(),
		opts:      []cmp.Option{allowVisibility, ignoreLocker, cmp.Comparer(pb.Equal), equalTable},
		wantPanic: "cannot handle unexported field",
	}, {
		label:     label,
		x:         createDirt(),
		y:         createDirt(),
		opts:      []cmp.Option{allowVisibility, transformProtos, ignoreLocker, cmp.Comparer(pb.Equal), equalTable},
		wantEqual: true,
	}, {
		label: label,
		x: func() ts.Dirt {
			d := createDirt()
			d.SetTable(ts.CreateMockTable([]string{"a", "c"}))
			d.Proto = pb.Dirt{Stringer: pb.Stringer{X: "blah"}}
			return d
		}(),
		y: func() ts.Dirt {
			d := createDirt()
			d.Discord = 500
			d.SetWizard(map[string]*pb.Wizard{
				"harry": {Stringer: pb.Stringer{X: "otter"}},
			})
			return d
		}(),
		opts:      []cmp.Option{allowVisibility, transformProtos, ignoreLocker, cmp.Comparer(pb.Equal), equalTable},
		wantEqual: false,
	}}
}

func project4Tests() []test {
	const label = "Project4"

	allowVisibility := cmp.AllowUnexported(
		ts.Cartel{},
		ts.Headquarter{},
		ts.Poison{},
	)

	transformProtos := cmp.Transformer("λ", func(x pb.Restrictions) *pb.Restrictions {
		return &x
	})

	createCartel := func() ts.Cartel {
		var p ts.Poison
		p.SetPoisonType(5)
		p.SetExpiration(now)
		p.SetManufacturer("acme")

		var hq ts.Headquarter
		hq.SetID(5)
		hq.SetLocation("moon")
		hq.SetSubDivisions([]string{"alpha", "bravo", "charlie"})
		hq.SetMetaData(&pb.MetaData{Stringer: pb.Stringer{X: "metadata"}})
		hq.SetPublicMessage([]byte{1, 2, 3, 4, 5})
		hq.SetHorseBack("abcdef")
		hq.SetStatus(44)

		var c ts.Cartel
		c.Headquarter = hq
		c.SetSource("mars")
		c.SetCreationTime(now)
		c.SetBoss("al capone")
		c.SetPoisons([]*ts.Poison{&p})

		return c
	}

	return []test{{
		label:     label,
		x:         createCartel(),
		y:         createCartel(),
		wantPanic: "cannot handle unexported field",
	}, {
		label:     label,
		x:         createCartel(),
		y:         createCartel(),
		opts:      []cmp.Option{allowVisibility, cmp.Comparer(pb.Equal)},
		wantPanic: "cannot handle unexported field",
	}, {
		label:     label,
		x:         createCartel(),
		y:         createCartel(),
		opts:      []cmp.Option{allowVisibility, transformProtos, cmp.Comparer(pb.Equal)},
		wantEqual: true,
	}, {
		label: label,
		x: func() ts.Cartel {
			d := createCartel()
			var p1, p2 ts.Poison
			p1.SetPoisonType(1)
			p1.SetExpiration(now)
			p1.SetManufacturer("acme")
			p2.SetPoisonType(2)
			p2.SetManufacturer("acme2")
			d.SetPoisons([]*ts.Poison{&p1, &p2})
			return d
		}(),
		y: func() ts.Cartel {
			d := createCartel()
			d.SetSubDivisions([]string{"bravo", "charlie"})
			d.SetPublicMessage([]byte{1, 2, 4, 3, 5})
			return d
		}(),
		opts:      []cmp.Option{allowVisibility, transformProtos, cmp.Comparer(pb.Equal)},
		wantEqual: false,
	}}
}

// BenchmarkBytes benchmarks the performance of performing Equal or Diff on
// large slices of bytes.
func BenchmarkBytes(b *testing.B) {
	// Create a list of PathFilters that never apply, but are evaluated.
	const maxFilters = 5
	var filters cmp.Options
	errorIface := reflect.TypeOf((*error)(nil)).Elem()
	for i := 0; i <= maxFilters; i++ {
		filters = append(filters, cmp.FilterPath(func(p cmp.Path) bool {
			return p.Last().Type().AssignableTo(errorIface) // Never true
		}, cmp.Ignore()))
	}

	type benchSize struct {
		label string
		size  int64
	}
	for _, ts := range []benchSize{
		{"4KiB", 1 << 12},
		{"64KiB", 1 << 16},
		{"1MiB", 1 << 20},
		{"16MiB", 1 << 24},
	} {
		bx := append(append(make([]byte, ts.size/2), 'x'), make([]byte, ts.size/2)...)
		by := append(append(make([]byte, ts.size/2), 'y'), make([]byte, ts.size/2)...)
		b.Run(ts.label, func(b *testing.B) {
			// Iteratively add more filters that never apply, but are evaluated
			// to measure the cost of simply evaluating each filter.
			for i := 0; i <= maxFilters; i++ {
				b.Run(fmt.Sprintf("EqualFilter%d", i), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(2 * ts.size)
					for j := 0; j < b.N; j++ {
						cmp.Equal(bx, by, filters[:i]...)
					}
				})
			}
			for i := 0; i <= maxFilters; i++ {
				b.Run(fmt.Sprintf("DiffFilter%d", i), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(2 * ts.size)
					for j := 0; j < b.N; j++ {
						cmp.Diff(bx, by, filters[:i]...)
					}
				})
			}
		})
	}
}
