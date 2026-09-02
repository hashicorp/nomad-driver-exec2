// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shoenig/test/must"
)

// verifies that fixpipe recreates the logs directory and FIFO 
// when the entire alloc-mounts directory is gone, as happens after a host reboot.
func Test_fixpipe_missing_dir(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root privileges")
	}

	// path whose parent directory does NOT exist — the exact post-reboot state
	dir := filepath.Join(t.TempDir(), "alloc", "logs")
	path := filepath.Join(dir, ".task.stdout.fifo")

	must.NoError(t, fixpipe(path, os.Getuid(), os.Getgid()))

	info, err := os.Stat(path)
	must.NoError(t, err)
	must.True(t, info.Mode()&os.ModeNamedPipe != 0, must.Sprint("expected a named pipe"))
}

// verifies that fixpipe recreates the FIFO when the
// parent directory exists but the FIFO file itself is missing.
func Test_fixpipe_missing_fifo(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root privileges")
	}

	dir := filepath.Join(t.TempDir(), "alloc", "logs")
	must.NoError(t, os.MkdirAll(dir, 0o777))
	path := filepath.Join(dir, ".task.stdout.fifo")

	// directory exists but FIFO does not
	must.NoError(t, fixpipe(path, os.Getuid(), os.Getgid()))

	info, err := os.Stat(path)
	must.NoError(t, err)
	must.True(t, info.Mode()&os.ModeNamedPipe != 0, must.Sprint("expected a named pipe"))
}

// verifies that calling fixpipe when the FIFO already
// exists (the normal non-reboot path) does not return an error.
func Test_fixpipe_idempotent(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root privileges")
	}

	dir := filepath.Join(t.TempDir(), "alloc", "logs")
	path := filepath.Join(dir, ".task.stdout.fifo")

	must.NoError(t, fixpipe(path, os.Getuid(), os.Getgid()))
	// calling again must not error — FIFO already exists
	must.NoError(t, fixpipe(path, os.Getuid(), os.Getgid()))
}

func Test_split(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		paths []string
		cmds  []string
	}{
		{
			name:  "env",
			args:  []string{"--", "env"},
			paths: nil,
			cmds:  []string{"env"},
		},
		{
			name:  "cat",
			args:  []string{"/etc/passwd:r", "--", "cat", "/etc/passwd"},
			paths: []string{"/etc/passwd:r"},
			cmds:  []string{"cat", "/etc/passwd"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths, cmds := split(tc.args)
			must.Eq(t, tc.paths, paths)
			must.Eq(t, tc.cmds, cmds)
		})
	}
}
