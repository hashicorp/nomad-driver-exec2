// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

func TestTrackCPU_Percent(t *testing.T) {
	tcpu := new(TrackCPU)

	// initial -> all zeros
	user, system, total := tcpu.Percent(1e12, 1e12, 1e12)
	must.Eq(t, 0, user)
	must.Eq(t, 0, system)
	must.Eq(t, 0, total)

	// do some work
	time.Sleep(1 * time.Millisecond)

	// we spent time doing work
	user, system, total = tcpu.Percent(1e14, 2e14, 3e14)
	must.Between(t, 1, user, 2)
	must.Between(t, 2, system, 3)
	must.Between(t, 3, total, 4)
}
