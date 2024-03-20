// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package process

import (
	"syscall"
	"testing"

	"github.com/shoenig/test/must"
)

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
