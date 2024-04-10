// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

func writeStatus(t *testing.T, code int) string {
	taskDir := t.TempDir()
	path := filepath.Join(taskDir, "local", ".exit_status.txt")
	must.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must.NoError(t, os.WriteFile(path, []byte(strconv.FormatInt(int64(code), 10)), 0o644))
	return taskDir
}

func TestPID_Wait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// when waiting via PID (the orphan case) we get the exit code from
	// the .exit_status.txt file written by the shim upon task process exit
	taskDir := writeStatus(t, 0)

	start := time.Now()

	cmd := exec.CommandContext(ctx, "sleep", ".1s")
	must.NoError(t, cmd.Start())

	waitCh := WaitPID(cmd.Process.Pid, taskDir).Wait()
	result := <-waitCh

	// ensure the pidfd_open + poll magic works; we actually wait for the
	// process to die before the plugin asks about its exit status
	must.Greater(t, 100*time.Millisecond, time.Since(start))
	must.NoError(t, result.Err)
	must.Zero(t, result.ExitCode)
}

func TestPID_WaitFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// when waiting via PID (the orphan case) we get the exit code from
	// the .exit_status.txt file written by the shim upon task process exit
	taskDir := writeStatus(t, 1)

	cmd := exec.CommandContext(ctx, "sleep", "abc")
	must.NoError(t, cmd.Start())

	waitCh := WaitPID(cmd.Process.Pid, taskDir).Wait()
	result := <-waitCh

	must.NoError(t, result.Err)
	must.One(t, result.ExitCode)
}
