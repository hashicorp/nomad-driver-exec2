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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

const timeout = 30 * time.Second

var (
	deadRe = regexp.MustCompile(`group\s+0\s+0\s+0\s+1\s+0\s+0\s+0`)
)

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
	t.Logf("RUN '%s %s'", command, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, command, args...)
	b, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(b))
	if err != nil {
		t.Log("ERR:", err)
		t.Log("OUT:", output)
		t.FailNow()
	}
	return output
}

func logs(t *testing.T, ctx context.Context, alloc string) string {
	const retries = 30
	for i := range retries {
		output := strings.TrimSpace(run(t, ctx, "nomad", "alloc", "logs", alloc))
		if output != "" {
			return output
		}
		t.Logf("got empty logs for alloc %s on attempt %d/%d", alloc, i, retries)
		time.Sleep(1 * time.Second)
	}
	t.FailNow()
	return ""
}

func logs2(t *testing.T, ctx context.Context, job, task string) string {
	const retries = 30
	for i := range retries {
		output := strings.TrimSpace(run(t, ctx, "nomad", "logs", "-job", job, task))
		if output != "" {
			return output
		}
		t.Logf("got empty logs for task %s/%s on attempt %d/%d", job, task, i, retries)
		time.Sleep(1 * time.Second)
	}
	t.FailNow()
	return ""
}

func purge(t *testing.T, ctx context.Context, job string) func() {
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

func TestPluginStarts(t *testing.T) {
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
	defer purge(t, ctx, "env")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/env.hcl")
	statusOutput := run(t, ctx, "nomad", "job", "status", "env")

	// env contains expected NOMAD_SHORT_ALLOC_ID
	alloc := allocFromJobStatus(t, statusOutput)
	containsAllocEnvRe := regexp.MustCompile(`NOMAD_SHORT_ALLOC_ID=` + alloc)

	// env contains sensible USER (dynamic)
	containsUserRe := regexp.MustCompile(`USER=nomad-\d+`)

	output := logs(t, ctx, alloc)
	must.RegexMatch(t, containsAllocEnvRe, output)
	must.RegexMatch(t, containsUserRe, output)
}

func TestBasic_Sleep(t *testing.T) {
	ctx := setup(t)
	defer purge(t, ctx, "sleep")()

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
	defer purge(t, ctx, "http")()

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
	defer purge(t, ctx, "passwd")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/passwd.hcl")

	// make sure job is failing (cannot read /etc/passwd)
	time.Sleep(5 * time.Second)
	jobStatus := run(t, ctx, "nomad", "job", "status", "passwd")
	must.RegexMatch(t, deadRe, jobStatus)

	// stop the job and check complete
	stopOutput := run(t, ctx, "nomad", "job", "stop", "-purge", "passwd")
	must.StrContains(t, stopOutput, `finished with status "complete"`)
}

func TestBasic_Cgroup(t *testing.T) {
	ctx := setup(t)
	defer purge(t, ctx, "cgroup")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/cgroup.hcl")

	statusOutput := run(t, ctx, "nomad", "job", "status", "cgroup")

	alloc := allocFromJobStatus(t, statusOutput)
	cgroupRe := regexp.MustCompile(`0::/nomad\.slice/share.slice/` + alloc + `.+\.cat\.scope`)

	output := logs(t, ctx, alloc)
	must.RegexMatch(t, cgroupRe, output)
}

func TestBasic_Bridge(t *testing.T) {
	ctx := setup(t)
	defer purge(t, ctx, "bridge")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/bridge.hcl")

	serviceInfo := run(t, ctx, "nomad", "service", "info", "homepage")
	addressRe := regexp.MustCompile(`([\d]+\.[\d]+.[\d]+\.[\d]+:[\d]+)`)

	m := addressRe.FindStringSubmatch(serviceInfo)
	must.SliceLen(t, 2, m, must.Sprint("expected to find address"))
	address := m[1]

	// curl service address
	curlOutput := run(t, ctx, "curl", "-s", address)
	must.StrContains(t, curlOutput, "<title>bridge mode</title>")
}

func TestBasic_ProcessNamespace(t *testing.T) {
	ctx := setup(t)
	defer purge(t, ctx, "ps")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/ps.hcl")

	logs := logs2(t, ctx, "ps", "ps")
	lines := strings.Split(logs, "\n") // header and ps
	must.SliceLen(t, 2, lines, must.Sprintf("expected 2 lines, got %q", logs))
}

func TestBasic_Exit7(t *testing.T) {
	ctx := setup(t)
	defer purge(t, ctx, "exit7")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/exit7.hcl")

	// make sure job is failing (script returns exit code 7)
	time.Sleep(5 * time.Second)
	jobStatus := run(t, ctx, "nomad", "job", "status", "exit7")
	must.RegexMatch(t, deadRe, jobStatus)

	// stop the job and check complete
	stopOutput := run(t, ctx, "nomad", "job", "stop", "-purge", "exit7")
	must.StrContains(t, stopOutput, `finished with status "complete"`)
}

func TestBasic_Resources(t *testing.T) {
	ctx := setup(t)
	defer purge(t, ctx, "resources")()

	_ = run(t, ctx, "nomad", "job", "run", "./jobs/resources.hcl")

	t.Run("memory.max", func(t *testing.T) {
		output := logs2(t, ctx, "resources", "memory.max")
		v, err := strconv.Atoi(strings.TrimSpace(output))
		must.NoError(t, err)
		must.Eq(t, 209_715_200, v)
	})

	t.Run("memory.max.oversub", func(t *testing.T) {
		output := logs2(t, ctx, "resources", "memory.max.oversub")
		v, err := strconv.Atoi(strings.TrimSpace(output))
		must.NoError(t, err)
		must.Eq(t, 262_144_000, v)
	})

	t.Run("memory.low.oversub", func(t *testing.T) {
		output := logs2(t, ctx, "resources", "memory.low.oversub")
		v, err := strconv.Atoi(strings.TrimSpace(output))
		must.NoError(t, err)
		must.Eq(t, 157_286_400, v)
	})

	t.Run("cpu.max", func(t *testing.T) {
		output := logs2(t, ctx, "resources", "cpu.max")
		s := strings.Fields(output)[0]
		v, err := strconv.Atoi(s)
		must.NoError(t, err)
		// gave it cpu=1000 which is (proably) less than 1 core
		must.Less(t, 100_000, v)
	})

	t.Run("cpu.max.cores", func(t *testing.T) {
		output := logs2(t, ctx, "resources", "cpu.max.cores")
		s := strings.Fields(output)[0]
		v, err := strconv.Atoi(s)
		must.NoError(t, err)
		must.Positive(t, v)
	})
}
