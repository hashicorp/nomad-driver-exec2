// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoenig/test/must"
)

func TestExeStopCgroupKillError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		signalErr error
	}{
		{name: "signal succeeds"},
		{name: "signal fails", signalErr: errors.New("signal failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cgroup := t.TempDir()
			signals := &stopTestSignaler{err: tc.signalErr}
			e := &exe{
				env:     &Environment{Cgroup: cgroup},
				signals: signals,
			}

			err := e.Stop("sigterm", 0)
			must.Eq(t, "sigterm", signals.received)
			must.ErrorContains(t, err, "failed to kill cgroup")
			must.True(t, errors.Is(err, os.ErrNotExist))
			var pathErr *os.PathError
			must.True(t, errors.As(err, &pathErr))
			must.Eq(t, filepath.Join(cgroup, "cgroup.kill"), pathErr.Path)
			if tc.signalErr != nil {
				must.True(t, errors.Is(err, tc.signalErr))
			}
		})
	}
}

type stopTestSignaler struct {
	err      error
	received string
}

func (s *stopTestSignaler) Send(signal string) error {
	s.received = signal
	return s.err
}
