// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"fmt"
	"os"
	"syscall"

	"github.com/hashicorp/nomad/plugins/drivers"
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

// WaitPID is able to wait on a given specific PID. We must lookup the
// process and also send a signal(0) to make sure it is actually still alive
// before waiting on it.
func WaitPID(pid int) Waiter {
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
	proc, _ := os.FindProcess(w.pid) // never errors on unix

	// send a 0 signal to detect if the process is still alive
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// TODO(shoenig): there is still a way to get the exit code and signal
		// of a recently deceased process by reading /proc; we should look into
		// that after Nomad 1.8 Beta since Client restarts are a thing that we
		// support.
		//
		// See what the pledge driver is doing for inspiration in
		// https://github.com/shoenig/nomad-pledge-driver/blob/v0.3.0/pkg/resources/process/wait.go#L82-L105
		// and make sure it works correctly.
		ch <- &drivers.ExitResult{
			ExitCode: -1,
			Err:      fmt.Errorf("task is gone: %w", err),
		}
		return
	}

	// from here we can just use the logic for waiting on an os.Process
	waiter := &procWaiter{p: proc}
	waiter.wait(ch)
}
