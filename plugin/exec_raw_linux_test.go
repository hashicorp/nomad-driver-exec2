// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"github.com/hashicorp/nomad/plugins/drivers"
)

// Ensure Plugin implements ExecTaskStreamingRawDriver at compile time; Nomad's
// gRPC server uses this interface to stream nomad alloc exec sessions.
var _ drivers.ExecTaskStreamingRawDriver = (*Plugin)(nil)
