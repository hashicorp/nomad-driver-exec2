// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"fmt"
	"os"
	"strings"

	"github.com/shoenig/go-landlock"
)

// Bundle tokens name go-landlock's built-in path sets. Each expands to an
// environment-specific set of paths that go-landlock resolves when the sandbox
// is applied. They travel in the unveil list as opaque strings so the driver 
// can include them without depending on go-landlock.
const (
	UnveilShared = "@shared" // shared libraries ( /lib, /usr/lib, ...)
	UnveilStdio  = "@stdio"  // standard I/O devices (/dev/null, /dev/zero, ...)
	UnveilDNS    = "@dns"    // name resolution files (/etc/resolv.conf, ...)
	UnveilCerts  = "@certs"  // TLS trust store (/etc/ssl/certs, ...)
)

// isProcSelfPath reports whether path is a descendant of /proc/self or /proc/thread-self 
func isProcSelfPath(path string) bool {
	return strings.HasPrefix(path, "/proc/self/") || strings.HasPrefix(path, "/proc/thread-self/")
}

func lockdown(elements []string) error {
	paths, err := convert(elements)
	if err != nil {
		return err
	}

	return landlock.New(paths...).Lock(landlock.Mandatory)
}

func convert(elements []string) ([]*landlock.Path, error) {
	paths := make([]*landlock.Path, 0, len(elements))

	for _, elem := range elements {
		// bundle tokens name a go-landlock path set and carry no mode prefix
		if bundle, ok := landlockBundle(elem); ok {
			paths = append(paths, bundle)
			continue
		}

		idx := strings.LastIndex(elem, ":")
		if idx == -1 {
			return nil, fmt.Errorf("path %q does not contain mode prefix", elem)
		}

		mode := elem[0:idx]
		filepath := elem[idx+1:]

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

// landlockBundle maps a bundle token to its go-landlock path set.
func landlockBundle(token string) (*landlock.Path, bool) {
	switch token {
	case UnveilShared:
		return landlock.Shared(), true
	case UnveilStdio:
		return landlock.Stdio(), true
	case UnveilDNS:
		return landlock.DNS(), true
	case UnveilCerts:
		return landlock.Certs(), true
	default:
		return nil, false
	}
}
