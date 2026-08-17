// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"fmt"
	"os"
	"strings"

	"github.com/shoenig/go-landlock"
)

// virtualFS lists filesystem roots that are virtual (kernel-generated).
// Their inodes are namespace-scoped: os.Stat on a path like /proc/self/mountinfo
// follows the /proc/self symlink to the shim's own PID inode, which does not
// exist in the task's private namespace after unshare --mount-proc.
// All paths under these roots must be treated as directories without stat(2).
var virtualFS = []string{
	"/proc",
	"/sys",
}

// virtualFSRoot returns the virtual filesystem root that contains path,
// or "" if path is not under any known virtual filesystem.
func virtualFSRoot(path string) string {
	for _, root := range virtualFS {
		if path == root || strings.HasPrefix(path, root+"/") {
			return root
		}
	}
	return ""
}

// When the nomad binary is invoked as exec2-shim, the format is
// nomad exec2-shim [path, [...]] -- [commands, [...]]
// so basically we need to find the first instance of '--' and split on that
func split(args []string) ([]string, []string) {
	var (
		paths    []string
		commands []string
	)

	index := 0
	for ; index < len(args); index++ {
		if args[index] == "--" {
			index++
			break
		}
		paths = append(paths, args[index])
	}

	for ; index < len(args); index++ {
		commands = append(commands, args[index])
	}

	return paths, commands
}

func lockdown(defaults bool, elements []string) error {
	paths, err := convert(elements)
	if err != nil {
		return err
	}

	if defaults {
		paths = append(paths, landlock.Shared())
		paths = append(paths, landlock.Stdio())
		paths = append(paths, landlock.DNS())
		paths = append(paths, landlock.Certs())
		paths = append(paths,
			landlock.Dir("/bin", "rx"),
			landlock.Dir("/usr/bin", "rx"),
			landlock.Dir("/usr/local/bin", "rx"),
		)
		// expose /proc read-only so runtimes (Go 1.25+, JVM, dotnet) can read
		// /proc/self/cgroup and /proc/self/mountinfo to discover their cgroup
		// CPU and memory limits. unshare --mount-proc creates 
		// an isolated /proc scoped to the task's PID namespace.
		paths = append(paths, landlock.Dir("/proc", "r"))
	}

	return landlock.New(paths...).Lock(landlock.Mandatory)
}

func convert(elements []string) ([]*landlock.Path, error) {
	paths := make([]*landlock.Path, 0, len(elements))

	for _, path := range elements {
		idx := strings.LastIndex(path, ":")
		if idx == -1 {
			return nil, fmt.Errorf("path %q does not contain mode prefix", path)
		}

		mode := path[0:idx]
		filepath := path[idx+1:]

		info, err := os.Stat(filepath)

		// Virtual filesystems (/proc, /sys) have namespace-scoped inodes.
		// /proc/self is a magic symlink that resolves to /proc/<shim-pid>;
		// that inode does not survive unshare --mount-proc into the task's
		// private namespace. We ignore stat errors for these paths and fall
		// back to File, which is correct for any leaf entry under /proc or
		// /sys (e.g. /proc/self/mountinfo). Paths that stat successfully
		// (e.g. "/proc", "/sys", "/proc/cpuinfo") take the normal dir/file
		// branch below.
		if err != nil {
			if virtualFSRoot(filepath) == "" {
				return nil, fmt.Errorf("failed to stat unveil path: %w", err)
			}
			paths = append(paths, landlock.File(filepath, mode))
			continue
		}

		if info.IsDir() {
			paths = append(paths, landlock.Dir(filepath, mode))
		} else {
			paths = append(paths, landlock.File(filepath, mode))
		}
	}

	return paths, nil
}
