// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package process

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type Exit struct {
	Code      int
	Interrupt int
	Err       error
}

type Waiter interface {
	Wait() *Exit
}

// WaitOnChild makes use of the os.Process handle to wait on the child process.
// This is the handle returned from directly launching the process through
// the exec package.
func WaitOnChild(p *os.Process) Waiter {
	return &execWaiter{p: p}
}

type execWaiter struct {
	p *os.Process
}

func (w *execWaiter) Wait() *Exit {
	ps, err := w.p.Wait()
	status := ps.Sys().(syscall.WaitStatus)
	code := ps.ExitCode()
	// TODO(shoenig): pledge driver added 128 to any negative code here
	// but ... why? do we need that?

	return &Exit{
		Code:      code,
		Interrupt: int(status),
		Err:       err,
	}
}

// WaitOnOrphan invokes the waitpid syscall on pid to wait on a process that
// the driver no longer has a direct handle on.
//
// Note that while we could use os.FindProcess to attempt to restore the handle
// of a live process, with waitpid we at least have a chance of recovering the
// exit status of a recently deceased child.
func WaitOnOrphan(pid int) Waiter {
	return &pidWaiter{pid: pid}
}

type pidWaiter struct {
	pid int
}

func (w *pidWaiter) Wait() *Exit {
	fd, err := openFD(w.pid)
	if err != nil {
		return &Exit{
			Code: -1,
			Err:  err,
		}
	}

	pollFD := []unix.PollFd{{Fd: fd}}
	const timeout = -1 // infinite

	// wait for the orphaned child to die
	// ignore the error; we expect it
	_, _ = unix.Poll(pollFD, timeout)

	// lookup exit code from /proc/<pid>/stat ?
	code, err := codeFromStat(w.pid)
	return &Exit{
		Code: code,
		Err:  err,
	}
}

// codeFromStat reads the exit code of pid from /proc/<pid>/stat
//
// See `man proc`.
// (52) exit_code  %d  (since Linux 3.5)
func codeFromStat(pid int) (int, error) {
	f := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	b, err := os.ReadFile(f)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 52 {
		return 0, fmt.Errorf("failed to read exit code from %q", f)
	}
	code, err := strconv.Atoi(fields[51])
	if err != nil {
		return 0, fmt.Errorf("failed to parse exit code from %q", f)
	}
	if code > 255 {
		// not sure why, read about waitpid
		code -= 255
	}
	return code, nil
}

// open fd for pid using pidfd_open
//
// https://www.man7.org/linux/man-pages/man2/pidfd_open.2.html
func openFD(pid int) (int32, error) {
	const syscallNumber = 434
	fd, _, err := syscall.Syscall(syscallNumber, uintptr(pid), uintptr(0), 0)
	if err != 0 {
		return 0, err
	}
	return int32(fd), nil
}
