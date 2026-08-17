// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package capabilities

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/moby/sys/capability"
)

// capNames maps normalized capability names (lowercase, no "cap_" prefix) to
// their kernel integer values. Built at init time from the moby/sys/capability
// library's authoritative list — automatically covering every capability the
// library knows about, with no manual maintenance required.
//
// On Linux, ListSupported queries the running kernel for the actual supported
// set. On other platforms, ListKnown returns the static compiled-in list
// (used when cross-compiling or running tests on non-Linux).
var capNames = func() map[string]uintptr {
	var list []capability.Cap
	switch runtime.GOOS {
	case "linux":
		list, _ = capability.ListSupported()
	default:
		list = capability.ListKnown()
	}
	m := make(map[string]uintptr, len(list))
	for _, c := range list {
		m[c.String()] = uintptr(c)
	}
	return m
}()

// ParseCap resolves a capability name to its kernel integer value. Names are
// normalized before lookup: case-insensitive and with or without the "cap_"
// prefix, so "NET_BIND_SERVICE", "cap_net_bind_service", "CAP_NET_BIND_SERVICE",
// and "net_bind_service" all resolve to the same value.
func ParseCap(name string) (uintptr, error) {
	v, ok := capNames[NormalizeCap(name)]
	if !ok {
		return 0, fmt.Errorf("unknown capability %q", name)
	}
	return v, nil
}

// Resolve resolves a slice of capability names to their kernel integer values.
// Returns the first error encountered for an unknown name.
func Resolve(names []string) ([]uintptr, error) {
	caps := make([]uintptr, 0, len(names))
	for _, name := range names {
		v, err := ParseCap(name)
		if err != nil {
			return nil, err
		}
		caps = append(caps, v)
	}
	return caps, nil
}

// NormalizeCap lowercases a capability name and strips any "cap_" prefix,
// accepting all four common formats: "net_bind_service", "NET_BIND_SERVICE",
// "cap_net_bind_service", "CAP_NET_BIND_SERVICE".
func NormalizeCap(name string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "cap_")
}
