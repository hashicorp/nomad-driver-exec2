// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"fmt"
	"os"
	"strings"

	"github.com/shoenig/go-landlock"
)

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
