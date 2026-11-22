// Copyright 2026, The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build race

package cmp_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const timeLocalRaceHelper = "GO_CMP_TIME_LOCAL_RACE_HELPER"

type timeRaceStringer struct{}

func (timeRaceStringer) String() string { return "" }

type timeRaceResource struct {
	timeRaceStringer
	WitnessedTime time.Time
}

func (r *timeRaceResource) clone() *timeRaceResource {
	r2 := *r
	return &r2
}

func TestDiffDoesNotRaceWithTimeLocalInitialization(t *testing.T) {
	if os.Getenv(timeLocalRaceHelper) == "1" {
		runTimeLocalRaceHelper()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestDiffDoesNotRaceWithTimeLocalInitialization$")
	cmd.Env = append(os.Environ(), timeLocalRaceHelper+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("race helper failed: %v\n%s", err, out)
	}
}

func runTimeLocalRaceHelper() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	witnessedTime := time.Now()
	// Ensure synchronizing the current time.Local is insufficient: witnessedTime
	// still refers to the original lazily initialized local location.
	time.Local = time.FixedZone("replacement", 0)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			r := timeRaceResource{WitnessedTime: witnessedTime}
			if diff := cmp.Diff(r, r.clone()); diff == "" {
				panic(fmt.Sprintf("cmp.Diff(%T, %T) returned an empty diff", r, r.clone()))
			}
		}
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			_ = witnessedTime.AppendFormat(nil, "")
		}
	}()
	wg.Wait()
}
