// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"fmt"
	"strings"
	"syscall"
)

// A Signaler is used to issue a signal to a process group.
type Signaler interface {
	// Send uses the kill syscall to issue a given signal.
	Send(signal string) error
}

func parse(s string) syscall.Signal {
	switch strings.ToLower(s) {
	case "sighup":
		return syscall.SIGHUP
	case "sigint":
		return syscall.SIGINT
	case "sigquit":
		return syscall.SIGQUIT
	case "sigtrap":
		return syscall.SIGTRAP
	case "sigabrt":
		return syscall.SIGABRT
	case "sigkill":
		return syscall.SIGKILL
	case "sigusr1":
		return syscall.SIGUSR1
	case "sigusr2":
		return syscall.SIGUSR2
	case "sigalrm":
		return syscall.SIGALRM
	case "sigterm":
		return syscall.SIGTERM
	case "sigstop":
		return syscall.SIGSTOP
	case "sigpwr":
		return syscall.SIGPWR
	default:
		// not much else we can do
		return syscall.Signal(0)
	}
}

// Signals returns a Signaler that issues real os signals.
func Signals(pid int) Signaler {
	return &system{pid: pid}
}

type system struct {
	pid int
}

func (s *system) Send(signal string) error {
	if s.pid <= 1 {
		return fmt.Errorf("not a valid PID to signal: %d", s.pid)
	}
	sig := parse(signal)
	group := -s.pid // signal the process group
	return syscall.Kill(group, sig)
}
