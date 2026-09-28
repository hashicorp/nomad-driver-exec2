// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"github.com/hashicorp/nomad/plugins/drivers"
)

// Ensure Plugin satisfies the low-level ExecTaskStreamingRawDriver interface at
// compile time.
var _ drivers.ExecTaskStreamingRawDriver = (*Plugin)(nil)
