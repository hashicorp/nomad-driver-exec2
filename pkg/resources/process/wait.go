// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"fmt"
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

func unrecoverable(msg string, err error) *drivers.ExitResult {
	return &drivers.ExitResult{
		ExitCode: -1,
		Err:      fmt.Errorf("%s: %w", msg, err),
	}
}

func (w *pidWaiter) wait(ch chan<- *drivers.ExitResult) {
	// send a 0 signal to detect if the process is still alive
	proc, _ := os.FindProcess(w.pid) // never errors on unix
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		ch <- unrecoverable("task process handle is gone", err)
		return
	}

	pidfd, err := openProcessFD(w.pid)
	if err != nil {
		ch <- unrecoverable("task process file descriptor cannot be opened", err)
		return
	}

	pollFD := []unix.PollFd{{
		Fd:     pidfd,
		Events: unix.POLLIN,
	}}
	const timeout = -1 // infinite

	// no need to check for an error
	_, _ = unix.Poll(pollFD, timeout)

	// retrieve code from file
	source := filepath.Join(w.taskdir, "local", exitStatusFile)
	code, err := getExitStatus(source)
	if err != nil {
		ch <- unrecoverable("task process exit status is missing", err)
		return
	}

	// finally send back the retreived exit status
	ch <- &drivers.ExitResult{
		ExitCode: code,
	}
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
