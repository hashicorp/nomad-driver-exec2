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

type latestStats struct {
	timestamp   time.Time
	utilization *resources.Utilization
}

// A Handle is used by the driver plugin to keep track of active tasks.
//
// Handle must be comletetly thread-safe; all operations must go through
// the lock.
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
	stats     latestStats
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
	return &Handle{
		pid:     runner.PID(),
		runner:  runner,
		config:  config,
		state:   drivers.TaskStateUnknown,
		clock:   libtime.SystemClock(),
		started: started,
		result:  nil,
	}
}

func (h *Handle) Stats() *resources.Utilization {
	const cacheTTL = 10 * time.Second

	h.lock.RLock()
	defer h.lock.RUnlock()

	elapsed := time.Since(h.stats.timestamp)
	if h.stats.utilization == nil || elapsed > cacheTTL {
		h.stats.utilization = h.runner.Stats()
		h.stats.timestamp = time.Now()
	}

	return h.stats.utilization
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

	h.lock.Lock()
	defer h.lock.Unlock()

	// a result has already been pushed through the channel and we just need to
	// read the existing value
	if h.result != nil {
		return
	}

	h.state = drivers.TaskStateExited
	h.result = result
	h.completed = h.clock.Now()

	if err := h.result.Err; err != nil {
		h.state = drivers.TaskStateUnknown
	}

	// close the channel; we are done waiting on our process and we can allow
	// other callers of Block to read the value we just set
	close(ch)
}

func (h *Handle) Signal(s string) error {
	return h.runner.Signal(s)
}

func (h *Handle) Stop(signal string, timeout time.Duration) error {
	return h.runner.Stop(signal, timeout)
}
