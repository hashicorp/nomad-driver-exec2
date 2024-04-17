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

// writeStatus will write the given status code to a .exit_status.txt file
// and return the filepath to that file.
//
// if delay is 0, this function blocks until the file exists
//
// otherwise, this function is non-blocking and creates the file in a goroutine
// after the delay has elapsed
func writeStatus(t *testing.T, code int, delay time.Duration) string {
	taskDir := t.TempDir()
	path := filepath.Join(taskDir, "local", ".exit_status.txt")
	must.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	switch delay {
	case 0:
		time.Sleep(delay)
		must.NoError(t, os.WriteFile(path, []byte(strconv.FormatInt(int64(code), 10)), 0o644))
	default:
		go func() {
			time.Sleep(delay)
			must.NoError(t, os.WriteFile(path, []byte(strconv.FormatInt(int64(code), 10)), 0o644))
		}()
	}
	return taskDir
}

func TestPID_Wait_already_exited(t *testing.T) {
	// in this case the child (shim) already exited and set a status code
	// file for us to read back
	taskDir := writeStatus(t, 7, 0)

	pid := 1 // does not matter
	waitCh := WaitPID(pid, taskDir).Wait()
	result := <-waitCh

	must.NoError(t, result.Err)
	must.Eq(t, 7, result.ExitCode)
}

func TestPID_Wait_poll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()

	cmd := exec.CommandContext(ctx, "sleep", ".1s")
	must.NoError(t, cmd.Start())

	// when waiting via PID (the orphan case) we get the exit code from
	// the .exit_status.txt file written by the shim upon task process exit
	//
	// since we aren't running in the context of the real shim here, just
	// fudge an exit code status file ... but wait long enough for the Wait()
	// below to actually start polling the process we started above
	//
	// please never use this test case as inspiration
	taskDir := writeStatus(t, 7, 90*time.Millisecond)

	waitCh := WaitPID(cmd.Process.Pid, taskDir).Wait()
	result := <-waitCh

	// ensure the pidfd_open + poll magic works; we actually wait for the
	// process to die before the plugin asks about its exit status which
	// we retrive from the .exit_status.txt file
	must.Greater(t, 100*time.Millisecond, time.Since(start))
	must.NoError(t, result.Err)
	must.Eq(t, 7, result.ExitCode)
}
