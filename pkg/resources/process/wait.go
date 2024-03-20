// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/hashicorp/nomad/plugins/drivers"
	"golang.org/x/sys/unix"
)

type WaitCh <-chan *drivers.ExitResult

type Waiter interface {
	Wait() WaitCh
}

// WaitOnChild makes use of the os.Process handle to wait on the child process.
// This is the handle returned from directly launching the process through
// the exec package.
func WaitOnChild(p *os.Process) Waiter {
	return &execWaiter{
		p:  p,
		ch: make(chan *drivers.ExitResult),
	}
}

type execWaiter struct {
	p  *os.Process
	ch chan *drivers.ExitResult
}

func (w *execWaiter) Wait() WaitCh {
	go w.wait(w.ch)
	return w.ch
}

func (w *execWaiter) wait(ch chan<- *drivers.ExitResult) {
	defer close(ch)

	ps, err := w.p.Wait()

	var signal = syscall.Signal(0)
	if ps.Sys() != nil {
		status := ps.Sys().(syscall.WaitStatus)
		signal = status.Signal()
	}

	ch <- &drivers.ExitResult{
		ExitCode: ps.ExitCode(),
		Signal:   int(signal),
		Err:      err,
	}
}

// WaitOnOrphan invokes the waitpid syscall on pid to wait on a process that
// the driver no longer has a direct handle on.
//
// Note that while we could use os.FindProcess to attempt to restore the handle
// of a live process, with waitpid we at least have a chance of recovering the
// exit status of a recently deceased child.
func WaitOnOrphan(pid int) Waiter {
	return &pidWaiter{
		pid: pid,
		ch:  make(chan *drivers.ExitResult),
	}
}

type pidWaiter struct {
	pid int
	ch  chan *drivers.ExitResult
}

func (w *pidWaiter) Wait() WaitCh {
	go w.wait(w.ch)
	return w.ch
}

func (w *pidWaiter) wait(ch chan<- *drivers.ExitResult) {
	defer close(ch)

	fd, err := openFD(w.pid)
	if err != nil {
		ch <- &drivers.ExitResult{
			ExitCode: -1,
			Err:      err,
		}
		return
	}

	pollFD := []unix.PollFd{{Fd: fd}}
	const timeout = -1 // infinite

	// wait for the orphaned child to die
	// ignore the error; we expect it
	_, _ = unix.Poll(pollFD, timeout)

	// lookup exit code from /proc/<pid>/stat ?
	code, err := codeFromStat(w.pid)
	ch <- &drivers.ExitResult{
		ExitCode: code,
		Err:      err,
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
