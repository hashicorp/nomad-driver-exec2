// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

func TestPID_Wait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()

	cmd := exec.CommandContext(ctx, "sleep", ".1s")
	must.NoError(t, cmd.Start())

	waitCh := WaitPID(cmd.Process.Pid).Wait()
	result := <-waitCh

	must.Greater(t, 100*time.Millisecond, time.Since(start))
	must.NoError(t, result.Err)
	must.Zero(t, result.ExitCode)
}

func TestPID_WaitFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "sleep", "abc") // exit 1
	must.NoError(t, cmd.Start())

	waitCh := WaitPID(cmd.Process.Pid).Wait()
	result := <-waitCh

	must.NoError(t, result.Err)
	must.One(t, result.ExitCode)
}
