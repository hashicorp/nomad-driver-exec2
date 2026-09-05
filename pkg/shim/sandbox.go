// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"fmt"
	"os"
	"strings"

	"github.com/shoenig/go-landlock"
)

// isProcSelfPath reports whether path is a descendant of /proc/self or /proc/thread-self 
func isProcSelfPath(path string) bool {
	return strings.HasPrefix(path, "/proc/self/") || strings.HasPrefix(path, "/proc/thread-self/")
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

		// /proc/self/* and /proc/thread-self/* contain PID-scoped magic symlinks.
		// go-landlock registers rules via O_PATH which pins the inode at the
		// time of the open — resolving /proc/self to /proc/<shim-pid>. After
		// unshare --mount-proc the task's private /proc has different inodes,
		// making the pinned inode unreachable (EPERM). Promote these paths to
		// Dir("/proc", mode) so the rule covers the whole /proc tree by its
		// stable directory inode instead.
		if isProcSelfPath(filepath) {
			paths = append(paths, landlock.Dir("/proc", mode))
			continue
		}

		info, err := os.Stat(filepath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat unveil path: %w", err)
		}

		if info.IsDir() {
			paths = append(paths, landlock.Dir(filepath, mode))
		} else {
			paths = append(paths, landlock.File(filepath, mode))
		}
	}

	return paths, nil
}
