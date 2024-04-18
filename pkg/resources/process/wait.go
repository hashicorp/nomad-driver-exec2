// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/hashicorp/nomad/plugins/drivers"
	"golang.org/x/sys/unix"
)

type WaitCh chan *drivers.ExitResult

type Waiter interface {
	Wait() WaitCh
}

// WaitProc makes use of the os.Process handle to wait on the child process.
// This is the handle returned from directly launching the process through
// the exec package.
func WaitProc(p *os.Process) Waiter {
	return &procWaiter{
		p:  p,
		ch: make(chan *drivers.ExitResult),
	}
}

type procWaiter struct {
	p  *os.Process
	ch chan *drivers.ExitResult
}

func (w *procWaiter) Wait() WaitCh {
	go w.wait(w.ch)
	return w.ch
}

func (w *procWaiter) wait(ch chan<- *drivers.ExitResult) {
	ps, err := w.p.Wait()

	var (
		signal = 0
		code   = ps.ExitCode()
	)

	if code == 0 && err == nil {
		ch <- &drivers.ExitResult{
			ExitCode: 0,
			Signal:   0,
			Err:      nil,
		}
		return
	}

	if ps != nil && ps.Sys() != nil {
		status := ps.Sys().(syscall.WaitStatus)
		if status.Signaled() {
			signal = int(status.Signal())
			code = 128 + signal // preserve bash-ism
		}
	}

	ch <- &drivers.ExitResult{
		ExitCode: code,
		Signal:   signal,
		Err:      err,
	}
}

const (
	// exitStatusFile is written into the task directory upon the exit of the
	// parent task process. It contains the exit status in case the value needs
	// to be retrieved by the plugin (i.e. if the task process was orphaned).
	exitStatusFile = ".exit_status.txt"
)

// WaitPID is able to wait on a given specific PID. We must lookup the
// process and also send a signal(0) to make sure it is actually still alive
// before waiting on it.
func WaitPID(pid int, taskdir string) Waiter {
	return &pidWaiter{
		pid:     pid,
		taskdir: taskdir,
		ch:      make(chan *drivers.ExitResult),
	}
}

type pidWaiter struct {
	pid     int
	taskdir string
	ch      chan *drivers.ExitResult
}

func (w *pidWaiter) Wait() WaitCh {
	go w.wait(w.ch)
	return w.ch
}

// finished will either succesfully read the exit code from the given source
// file, or will return a -1 exit status with an error message if the file
// cannot be read for any reason.
func finished(source string, msg string) *drivers.ExitResult {
	code, err := getExitStatus(source)
	if err == nil {
		return &drivers.ExitResult{
			ExitCode: code,
		}
	}
	return &drivers.ExitResult{
		ExitCode: -1,
		Err:      errors.New(msg),
	}
}

func (w *pidWaiter) wait(ch chan<- *drivers.ExitResult) {
	// full path to the .exit_status.txt file
	source := filepath.Join(w.taskdir, "local", exitStatusFile)

	// attempt to acquire a pidFD on the PID of what may or may not still be
	// our task process
	pidFD, pidErr := openProcessFD(w.pid)
	if pidErr != nil {
		ch <- finished(source, "task process file descriptor cannot be opened")
		return
	}
	defer func() { _ = unix.Close(int(pidFD)) }()

	pollFD := []unix.PollFd{{
		Fd:     pidFD,
		Events: unix.POLLIN,
	}}
	const timeout = -1 // infinite

	// while we are holding the pidfd to *a* process of the same PID as the
	// process we launched before client restart, check again if the
	// .exit_status.txt file exists - which would indicate we're holding onto
	// the pidfd of a process we do not know or care about
	code, codeErr := getExitStatus(source)
	if codeErr == nil && code >= 0 {
		ch <- &drivers.ExitResult{
			ExitCode: code,
		}
	}

	// no need to check for an error
	_, _ = unix.Poll(pollFD, timeout)

	// the process has terminated; we should be able to read the
	// .exit_status.txt file the shim will have left behind for us
	ch <- finished(source, "task process complete but status is missing")
}

func getExitStatus(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return -1, err
	}
	code, err := strconv.Atoi(string(b))
	if err != nil {
		return -1, err
	}
	return code, nil
}

// openProcessFD returns a file descriptor for the process of the given PID. This FD
// can then later be used for other syscalls as a reference to the process.
//
// Good discussion about better native support in Go for this in
// https://github.com/golang/go/issues/62654
func openProcessFD(pid int) (int32, error) {
	const syscallNumber = 434 // sys_pidfd_open
	fd, _, err := syscall.Syscall(syscallNumber, uintptr(pid), uintptr(0), 0)
	if err != 0 {
		return 0, err
	}
	return int32(fd), nil
}
