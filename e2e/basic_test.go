// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

//go:build e2e

// Run these tests manually by setting the e2e tag when running go test, e.g.
//
//	➜ go test -tags=e2e -v
//
// For editing set: export GOFLAGS='--tags=e2e'

package shim

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

const timeout = 30 * time.Second

func pause() {
	if ci := os.Getenv("CI"); ci == "" {
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
}

func setup(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(func() {
		run(t, ctx, "nomad", "system", "gc")
		cancel()
	})
	pause()
	return ctx
}

func run(t *testing.T, ctx context.Context, command string, args ...string) string {
	t.Log("RUN", "command:", command, "args:", args)
	cmd := exec.CommandContext(ctx, command, args...)
	b, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(b))
	if err != nil {
		t.Log("ERR:", err)
		t.Log("OUT:", output)
	}
	return output
}

func stop(t *testing.T, ctx context.Context, job string) func() {
	return func() {
		t.Log("STOP", job)
		cmd := exec.CommandContext(ctx, "nomad", "job", "stop", "-purge", job)
		b, err := cmd.CombinedOutput()
		output := strings.TrimSpace(string(b))
		if err != nil {
			t.Log("ERR:", err)
			t.Log("OUT:", output)
		}
		pause()
	}
}

func allocFromJobStatus(t *testing.T, s string) string {
	re := regexp.MustCompile(`([[:xdigit:]]+)\s+([[:xdigit:]]+)\s+group`)
	matches := re.FindStringSubmatch(s)
	must.Len(t, 3, matches, must.Sprint("regex results", matches))
	return matches[1]
}

func TestBasic_Startup(t *testing.T) {
	ctx := setup(t)

	// can connect to nomad
	jobs := run(t, ctx, "nomad", "job", "status")
	must.Eq(t, "No running jobs", jobs)

	// exec2 plugin is present and healthy
	status := run(t, ctx, "nomad", "node", "status", "-self", "-verbose")
	exec2Re := regexp.MustCompile(`exec2\s+true\s+true\s+Healthy`)
	must.RegexMatch(t, exec2Re, status)
}

func TestBasic_Env(t *testing.T) {
	ctx := setup(t)
	defer stop(t, ctx, "env")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/env.hcl")
	statusOutput := run(t, ctx, "nomad", "job", "status", "env")

	// env contains expected NOMAD_SHORT_ALLOC_ID
	alloc := allocFromJobStatus(t, statusOutput)
	containsAllocEnvRe := regexp.MustCompile(`NOMAD_SHORT_ALLOC_ID=` + alloc)

	// env contains sensible USER (dynamic)
	containsUserRe := regexp.MustCompile(`USER=nomad-\d+`)

	logs := run(t, ctx, "nomad", "alloc", "logs", alloc)
	must.RegexMatch(t, containsAllocEnvRe, logs)
	must.RegexMatch(t, containsUserRe, logs)
}

func TestBasic_Sleep(t *testing.T) {
	ctx := setup(t)
	defer stop(t, ctx, "sleep")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/sleep.hcl")

	// no log output, make sure jbo is running
	jobStatus := run(t, ctx, "nomad", "job", "status", "sleep")
	runningRe := regexp.MustCompile(`Status\s+=\s+running`)
	must.RegexMatch(t, runningRe, jobStatus)

	// stop the job
	stopOutput := run(t, ctx, "nomad", "job", "stop", "sleep")
	must.StrContains(t, stopOutput, `finished with status "complete"`)

	// check job is stopped
	stopStatus := run(t, ctx, "nomad", "job", "status", "sleep")
	deadRe := regexp.MustCompile(`Status\s+=\s+dead\s+\(stopped\)`)
	must.RegexMatch(t, deadRe, stopStatus)
}

func TestBasic_HTTP(t *testing.T) {
	ctx := setup(t)
	defer stop(t, ctx, "http")

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/http.hcl")

	// make sure job is running
	jobStatus := run(t, ctx, "nomad", "job", "status", "http")
	runningRe := regexp.MustCompile(`Status\s+=\s+running`)
	must.RegexMatch(t, runningRe, jobStatus)

	// curl localhost:8181
	curlOutput := run(t, ctx, "curl", "-s", "localhost:8181")
	must.StrContains(t, curlOutput, `<title>example</title>`)

	// stop the job
	stopOutput := run(t, ctx, "nomad", "job", "stop", "http")
	must.StrContains(t, stopOutput, `finished with status "complete"`)

	// check job is stopped
	stopStatus := run(t, ctx, "nomad", "job", "status", "http")
	stoppedRe := regexp.MustCompile(`Status\s+=\s+dead\s+\(stopped\)`)
	must.RegexMatch(t, stoppedRe, stopStatus)
}

func TestBasic_Passwd(t *testing.T) {
	ctx := setup(t)
	defer stop(t, ctx, "passwd")

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/passwd.hcl")

	// make sure job is failing (cannot read /etc/passwd)
	time.Sleep(5 * time.Second)
	jobStatus := run(t, ctx, "nomad", "job", "status", "passwd")
	deadRe := regexp.MustCompile(`group\s+0\s+0\s+0\s+1\s+0\s+0\s+0`)
	must.RegexMatch(t, deadRe, jobStatus)

	// stop the job
	stopOutput := run(t, ctx, "nomad", "job", "stop", "-purge", "passwd")
	must.StrContains(t, stopOutput, `finished with status "complete"`)
}

func TestBasic_Cgroup(t *testing.T) {
	ctx := setup(t)
	defer stop(t, ctx, "cgroup")

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/cgroup.hcl")

	// make sure job is complete
	time.Sleep(5 * time.Second)
	statusOutput := run(t, ctx, "nomad", "job", "status", "cgroup")

	alloc := allocFromJobStatus(t, statusOutput)
	cgroupRe := regexp.MustCompile(`0::/nomad\.slice/share.slice/` + alloc + `.+\.cat\.scope`)

	logs := run(t, ctx, "nomad", "alloc", "logs", alloc)
	must.RegexMatch(t, cgroupRe, logs)
}
