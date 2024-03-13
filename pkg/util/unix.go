// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package util

import "runtime"

// IsLinuxOS returns true if the operating system is some Linux distribution.
func IsLinuxOS() bool {
	return runtime.GOOS == "linux"
}
