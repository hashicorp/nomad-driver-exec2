// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"testing"

	"github.com/shoenig/go-landlock"
	"github.com/shoenig/test/must"
)

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

func Test_virtualFSRoot(t *testing.T) {
	cases := []struct {
		path   string
		expect string
	}{
		{"/proc/self/mountinfo", "/proc"},
		{"/proc/self/cgroup", "/proc"},
		{"/proc/cpuinfo", "/proc"},
		{"/proc", "/proc"},
		{"/sys/fs/cgroup", "/sys"},
		{"/sys", "/sys"},
		{"/etc/passwd", ""},
		{"/procfake", ""},
		{"/usr/local/bin", ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			must.Eq(t, tc.expect, virtualFSRoot(tc.path))
		})
	}
}

func Test_convert_virtual(t *testing.T) {
	// /proc/self/mountinfo must not be stat(2)'d — the shim's PID inode does
	// not survive unshare --mount-proc. convert() must emit Dir without error.
	paths, err := convert([]string{
		"r:/proc/self/mountinfo",
		"r:/proc/cpuinfo",
		"r:/sys/fs/cgroup",
	})
	must.NoError(t, err)
	must.Len(t, 3, paths)

	// All three must be Dir rules (never File), regardless of what they look
	// like on the host filesystem.
	expected := []*landlock.Path{
		landlock.Dir("/proc/self/mountinfo", "r"),
		landlock.Dir("/proc/cpuinfo", "r"),
		landlock.Dir("/sys/fs/cgroup", "r"),
	}
	must.Eq(t, expected, paths)
}

func Test_convert_missing(t *testing.T) {
	// A non-virtual path that doesn't exist on disk must return an error.
	_, err := convert([]string{"r:/nonexistent/path/xyz"})
	must.Error(t, err)
}

func Test_convert_no_mode(t *testing.T) {
	// A path with no mode prefix must return an error.
	_, err := convert([]string{"/proc/cpuinfo"})
	must.Error(t, err)
}
