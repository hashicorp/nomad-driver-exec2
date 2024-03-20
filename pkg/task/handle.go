// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build linux

package task

import (
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/nomad-driver-exec2/pkg/resources"
	"github.com/hashicorp/nomad-driver-exec2/pkg/shim"
	"github.com/hashicorp/nomad/plugins/drivers"
	"oss.indeed.com/go/libtime"
)

// A Handle is used by the driver plugin to keep track of active tasks.
type Handle struct {
	lock sync.RWMutex

	runner    shim.ExecTwo
	config    *drivers.TaskConfig
	state     drivers.TaskState
	started   time.Time
	completed time.Time
	result    *drivers.ExitResult
	clock     libtime.Clock
	pid       int
}

func NewHandle(runner shim.ExecTwo, config *drivers.TaskConfig) (*Handle, time.Time) {
	clock := libtime.SystemClock()
	now := clock.Now()
	return &Handle{
		pid:     runner.PID(),
		runner:  runner,
		config:  config,
		state:   drivers.TaskStateRunning,
		clock:   clock,
		started: now,
		result:  nil,
	}, now
}

func RecreateHandle(runner shim.ExecTwo, config *drivers.TaskConfig, started time.Time) *Handle {
	clock := libtime.SystemClock()
	return &Handle{
		pid:     runner.PID(),
		runner:  runner,
		config:  config,
		state:   drivers.TaskStateUnknown,
		clock:   clock,
		started: started,
		result:  nil,
	}
}

func (h *Handle) Stats() resources.Utilization {
	h.lock.RLock()
	defer h.lock.RUnlock()
	return h.runner.Stats()
}

func (h *Handle) IsRunning() bool {
	h.lock.RLock()
	defer h.lock.RUnlock()
	return h.state == drivers.TaskStateRunning
}

func (h *Handle) Status() *drivers.TaskStatus {
	h.lock.RLock()
	defer h.lock.RUnlock()

	return &drivers.TaskStatus{
		ID:          h.config.ID,
		Name:        h.config.Name,
		State:       h.state,
		StartedAt:   h.started,
		CompletedAt: h.completed,
		ExitResult:  h.result,
		DriverAttributes: map[string]string{
			"pid": strconv.Itoa(h.pid),
		},
	}
}

func (h *Handle) Block() {
	ch := h.runner.WaitCh()
	result := <-ch
	// nl.Info("got result, in Handle.Block()", "code", result.ExitCode, "error", result.Err)

	h.lock.Lock()
	defer h.lock.Unlock()

	if h.result != nil {
		return
	}

	h.state = drivers.TaskStateExited
	h.result = result
	h.completed = h.clock.Now()

	if err := h.result.Err; err != nil {
		h.state = drivers.TaskStateUnknown
	}
}

func (h *Handle) Signal(s string) error {
	return h.runner.Signal(s)
}

func (h *Handle) Stop(signal string, timeout time.Duration) error {
	return h.runner.Stop(signal, timeout)
}
