// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"fmt"
	"syscall"
	"testing"

	"github.com/shoenig/test/must"
)

func TestSignals_Send_error(t *testing.T) {
	cases := []struct {
		pid int
		exp string
	}{
		{
			pid: -1,
			exp: "not a valid PID to signal: -1",
		},
		{
			pid: 0,
			exp: "not a valid PID to signal: 0",
		},
		{
			pid: 1,
			exp: "not a valid PID to signal: 1",
		},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("pid(%d)", tc.pid)
		t.Run(name, func(t *testing.T) {
			s := &system{pid: tc.pid}
			result := s.Send("none")
			must.EqError(t, result, tc.exp)
		})
	}
}

func TestSignals_parse(t *testing.T) {
	cases := []struct {
		name string
		exp  syscall.Signal
	}{
		{
			name: "sighup",
			exp:  syscall.SIGHUP,
		},
		{
			name: "SIGHUP",
			exp:  syscall.SIGHUP,
		},
		{
			name: "invalid",
			exp:  syscall.Signal(0),
		},
		{
			name: "sigint",
			exp:  syscall.SIGINT,
		},
		{
			name: "sigquit",
			exp:  syscall.SIGQUIT,
		},
		{
			name: "sigtrap",
			exp:  syscall.SIGTRAP,
		},
		{
			name: "sigabrt",
			exp:  syscall.SIGABRT,
		},
		{
			name: "sigkill",
			exp:  syscall.SIGKILL,
		},
		{
			name: "sigusr1",
			exp:  syscall.SIGUSR1,
		},
		{
			name: "sigusr2",
			exp:  syscall.SIGUSR2,
		},
		{
			name: "sigalrm",
			exp:  syscall.SIGALRM,
		},
		{
			name: "sigterm",
			exp:  syscall.SIGTERM,
		},
		{
			name: "sigstop",
			exp:  syscall.SIGSTOP,
		},
		{
			name: "sigpwr",
			exp:  syscall.SIGPWR,
		},
	}

	for _, tc := range cases {
		result := parse(tc.name)
		must.Eq(t, tc.exp, result)
	}
}
