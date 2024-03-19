// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package task

import (
	"time"

	"github.com/hashicorp/nomad/plugins/drivers"
)

// State is the runtime state encoded in the handle, returned to the Nomad
// client. Used to rebuild the task state and handler during recover.
type State struct {
	TaskConfig *drivers.TaskConfig
	StartedAt  time.Time
	PID        int
	Cancel     func()
}
