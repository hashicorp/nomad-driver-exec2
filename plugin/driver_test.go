// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/client/lib/cgroupslib"
	ctests "github.com/hashicorp/nomad/client/testutil"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	dtests "github.com/hashicorp/nomad/plugins/drivers/testutils"
	dstructs "github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/shoenig/test/must"
)

func newTestHarness(t *testing.T, pluginConfig *Config) *dtests.DriverHarness {
	logger := testlog.HCLogger(t)
	plugin := New(logger).(*Plugin)

	// set a base config with reasonable topology
	baseConfig := &base.Config{
		AgentConfig: &base.AgentConfig{
			Driver: &base.ClientDriverConfig{
				Topology: structs.MockWorkstationTopology(),
			},
		},
	}

	// encode and set plugin config
	must.NoError(t, base.MsgPackEncode(&baseConfig.PluginConfig, pluginConfig))
	must.NoError(t, plugin.SetConfig(baseConfig))

	// set initial fingerprint
	plugin.doFingerprint(exec.LookPath)

	// configure cgroups controllers
	procs := max(1, runtime.GOMAXPROCS(0)-1)
	must.NoError(t, cgroupslib.Init(logger, fmt.Sprintf("0-%d", procs)))

	// create a harness to run our plugin
	return dtests.NewDriverHarness(t, plugin)
}

func basicResources(allocID, taskName string) *drivers.Resources {
	if allocID == "" || taskName == "" {
		panic("test: allocID and taskName must be set")
	}

	return &drivers.Resources{
		NomadResources: &structs.AllocatedTaskResources{
			Memory: structs.AllocatedMemoryResources{
				MemoryMB: 100,
			},
			Cpu: structs.AllocatedCpuResources{
				CpuShares: 250,
			},
		},
		LinuxResources: &drivers.LinuxResources{
			CPUShares:        500,
			MemoryLimitBytes: 256 * 1024 * 1024,
			CpusetCgroupPath: cgroupslib.LinuxResourcesPath(allocID, taskName, false),
		},
	}
}

var debugExitResult = func(result *drivers.ExitResult) must.Setting {
	return must.Sprintf(
		"got code: %d, signal: %d, err: %v",
		result.ExitCode,
		result.Signal,
		result.Err,
	)
}

func TestFunctional_StartWait(t *testing.T) {
	ci.Parallel(t)

	pluginConfig := &Config{
		UnveilDefaults: true,
	}

	taskConfig := &TaskConfig{
		Command: "sleep",
		Args:    []string{"infinity"},
	}

	allocID := uuid.Generate()
	taskName := "start_wait_test_" + uuid.Short()

	task := &drivers.TaskConfig{
		User:      "nomad-80000",
		ID:        uuid.Generate(),
		Name:      taskName,
		AllocID:   allocID,
		Resources: basicResources(allocID, taskName),
	}

	must.NoError(t, task.EncodeConcreteDriverConfig(&taskConfig))

	harness := newTestHarness(t, pluginConfig)
	harness.MakeTaskCgroup(task.AllocID, task.Name)
	cleanup := harness.MkAllocDir(task, true)
	defer cleanup()

	// Start the task
	_, _, err := harness.StartTask(task)
	must.NoError(t, err)

	defer func() {
		_ = harness.DestroyTask(task.ID, true)
	}()

	// Attempt to wait on task
	waitCh, err := harness.WaitTask(context.Background(), task.ID)
	must.NoError(t, err)

	select {
	case <-waitCh:
		t.Fatal("task should not exit")
	case <-time.After(10 * time.Second):
	}
}

func TestFunctional_cases(t *testing.T) {
	ctests.RequireRoot(t)

	ci.Parallel(t)

	// various tests making assertions on exit code and log outputs
	//
	// note: all tasks must be batch and complete in under 10 seconds

	cases := []struct {
		name string

		// task config
		user    string
		command string
		args    []string
		unveil  []string
		capAdd  []string
		capDrop []string
		workDir string // relative or absolute path; empty defaults to NOMAD_TASK_DIR

		// plugin config
		unveilDefaults bool
		unveilByTask   bool
		unveilPaths    []string
		allowCaps      []string

		// expectations
		exp      *drivers.ExitResult
		stdoutRe *regexp.Regexp
		stderrRe *regexp.Regexp
	}{
		// run 'env' with default unveil paths
		{
			name:           "dynamic user",
			user:           "nomad-80000",
			command:        "env",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`USER=nomad-80000`),
		},
		{
			name:           "nobody user",
			user:           "nobody",
			command:        "env",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`USER=nobody`),
		},
		{
			name:           "root user",
			user:           "root",
			command:        "env",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`USER=root`),
		},
		// run 'cat /etc/passwd' with default unveil paths
		// (e.g. not even root can access it)
		{
			name:           "read /etc/passwd as dynamic using defaults",
			user:           "nomad-80000",
			command:        "cat",
			unveilDefaults: true,
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 1},
			stderrRe:       regexp.MustCompile(`cat: /etc/passwd: Permission denied`),
		},
		{
			name:           "read /etc/passwd as nobody using defaults",
			user:           "nobody",
			command:        "cat",
			unveilDefaults: true,
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 1},
			stderrRe:       regexp.MustCompile(`cat: /etc/passwd: Permission denied`),
		},
		{
			name:           "read /etc/passwd as root using defaults",
			user:           "root",
			command:        "cat",
			unveilDefaults: true,
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 1},
			stderrRe:       regexp.MustCompile(`cat: /etc/passwd: Permission denied`),
		},
		// run 'cat /etc/passwd' with custom unveil paths in plugin config
		// allowing any task to read /etc/passwd
		{
			name:           "read /etc/passwd as dynamic using custom paths via plugin",
			user:           "nomad-80000",
			command:        "cat",
			unveilDefaults: true,
			unveilPaths:    []string{"r:/etc/passwd"},
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`root:x:0:0:root:/root:/bin/bash`),
		},
		{
			name:           "read /etc/passwd as nobody using custom paths via plugin",
			user:           "nobody",
			command:        "cat",
			unveilDefaults: true,
			unveilPaths:    []string{"r:/etc/passwd"},
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`root:x:0:0:root:/root:/bin/bash`),
		},
		{
			name:           "read /etc/passwd as root using custom paths via plugin",
			user:           "root",
			command:        "cat",
			unveilDefaults: true,
			unveilPaths:    []string{"r:/etc/passwd"},
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`root:x:0:0:root:/root:/bin/bash`),
		},
		// run 'cat /etc/passwd' with custom unveil paths in task config and allow
		// doing so in plugin config
		{
			name:           "read /etc/passwd as dynamic using custom paths via task",
			user:           "nomad-80000",
			command:        "cat",
			unveilDefaults: true,
			unveilByTask:   true,
			unveil:         []string{"r:/etc/passwd"},
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`root:x:0:0:root:/root:/bin/bash`),
		},
		{
			name:           "read /etc/passwd as nobody using custom paths via task",
			user:           "nobody",
			command:        "cat",
			unveilDefaults: true,
			unveilByTask:   true,
			unveil:         []string{"r:/etc/passwd"},
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`root:x:0:0:root:/root:/bin/bash`),
		},
		{
			name:           "read /etc/passwd as root using custom paths via task",
			user:           "root",
			command:        "cat",
			unveilDefaults: true,
			unveilByTask:   true,
			unveil:         []string{"r:/etc/passwd"},
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`root:x:0:0:root:/root:/bin/bash`),
		},
		// stdout and stderr are routed to separate pipes — validates that
		// cmd.Stdout and cmd.Stderr are wired to OutPipe and ErrPipe respectively
		{
			name:           "stdout and stderr routed to correct pipes",
			user:           "nomad-80000",
			command:        "sh",
			unveilDefaults: true,
			args:           []string{"-c", "echo STDOUT_MARKER; echo STDERR_MARKER >&2"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`STDOUT_MARKER`),
			stderrRe:       regexp.MustCompile(`STDERR_MARKER`),
		},
		// try to execute a non-existent file
		{
			name:           "execute non-existent program",
			user:           "nomad-80000",
			command:        "/usr/bin/doesnotexist",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 127},
			stderrRe:       regexp.MustCompile(`failed to locate command "/usr/bin/doesnotexist": exec: "/usr/bin/doesnotexist": stat /usr/bin/doesnotexist: no such file or directory`),
		},
		// try to execute non-executable file
		{
			name:           "execute non-executable file",
			user:           "nomad-80000",
			command:        "/etc/os-release",
			unveilDefaults: true,
			unveilPaths:    []string{"rx:/etc"},
			exp:            &drivers.ExitResult{ExitCode: 127},
			stderrRe:       regexp.MustCompile(`failed to locate command "/etc/os-release": exec: "/etc/os-release": permission denied`),
		},
		// disable unveil_defaults and commands in /bin, /usr/bin should no
		// longer work
		{
			name:           "run 'env' as dynamic without default paths",
			user:           "nomad-80000",
			command:        "/usr/bin/env",
			unveilDefaults: false,
			exp:            &drivers.ExitResult{ExitCode: 1},
		},
		{
			name:           "run 'env' as nobody without default paths",
			user:           "nobody",
			command:        "/usr/bin/env",
			unveilDefaults: false,
			exp:            &drivers.ExitResult{ExitCode: 1},
		},
		{
			name:           "run 'env' as root without default paths",
			user:           "root",
			command:        "/usr/bin/env",
			unveilDefaults: false,
			exp:            &drivers.ExitResult{ExitCode: 1},
		},
		// write to task directory
		{
			name:           "write to task directory",
			user:           "nomad-80000",
			command:        "sh",
			unveilDefaults: true,
			unveilPaths:    []string{"r:/etc/hosts"},
			args:           []string{"-c", "cp /etc/hosts ${NOMAD_TASK_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		{
			name:           "write to alloc directory",
			user:           "nomad-80000",
			command:        "sh",
			unveilDefaults: true,
			unveilPaths:    []string{"r:/etc/hosts"},
			args:           []string{"-c", "cp /etc/hosts ${NOMAD_ALLOC_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		{
			name:           "write to secrets directory",
			user:           "nomad-80000",
			command:        "sh",
			unveilDefaults: true,
			unveilPaths:    []string{"r:/etc/hosts"},
			args:           []string{"-c", "cp /etc/hosts ${NOMAD_SECRETS_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		// fail to write to task directory with no defaults
		{
			name:           "write to task directory no defaults",
			user:           "nomad-81000",
			command:        "sh",
			unveilDefaults: false,
			unveilPaths:    []string{"r:/etc/hosts"},
			args:           []string{"-c", "cp /etc/hosts ${NOMAD_TASK_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 1},
		},
		{
			name:           "write to alloc directory no defaults",
			user:           "nomad-81000",
			command:        "sh",
			unveilDefaults: false,
			unveilPaths:    []string{"r:/etc/hosts"},
			args:           []string{"-c", "cp /etc/hosts ${NOMAD_ALLOC_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 1},
		},
		{
			name:           "write to secrets directory no defaults",
			user:           "nomad-80000",
			command:        "sh",
			unveilDefaults: false,
			unveilPaths:    []string{"r:/etc/hosts"},
			args:           []string{"-c", "cp /etc/hosts ${NOMAD_SECRETS_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 1},
		},
		// dyanmic id
		{
			name:           "id dynamic",
			user:           "nomad-89000",
			command:        "id",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`uid=89000 gid=89000 groups=89000`),
		},
		{
			name:           "id nobody",
			user:           "nobody",
			command:        "id",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`uid=65534 gid=65534 groups=65534`),
		},
		{
			name:           "id root",
			user:           "root",
			command:        "id",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`uid=0 gid=0 groups=0`),
		},
		{
			name:           "pid namespace",
			user:           "root",
			command:        "ps",
			args:           []string{"aux"},
			unveilDefaults: true,
			unveilPaths:    []string{"r:/proc", "r:/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`root\s+1.+ps aux`), // out ps is pid 1
		},
		// mount namespace has slave propagation — host mounts propagate into
		// the task not vice versa.
		// The kernel records slave propagation as "master:N" in mountinfo.
		{
			name:           "mount propagation slave",
			user:           "root",
			command:        "awk",
			args:           []string{"NR==1", "/proc/self/mountinfo"},
			unveilDefaults: true,
			unveilPaths:    []string{"r:/proc"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`master:\d+`),
		},
		// able to use TMPDIR
		{
			name:           "use TMPDIR",
			user:           "nomad-82000",
			command:        "mktemp",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`\w+/tmp/tmp\.\w+`),
		},
		// cap_add: a granted capability is present as ambient in the task process.
		// We read the ambient cap bitmask from /proc/self/status and verify it is
		// non-zero. CAP_NET_BIND_SERVICE is bit 10 → bitmask 0x0000000000000400.
		// /proc is unveiled so the PID-namespace /proc/self is reachable.
		{
			name:           "cap_add ambient cap is raised in task",
			user:           "nomad-80000",
			command:        "sh",
			args:           []string{"-c", "grep CapAmb /proc/self/status"},
			unveilDefaults: true,
			unveilPaths:    []string{"r:/proc"},
			allowCaps:      []string{"net_bind_service"},
			capAdd:         []string{"net_bind_service"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			// match any non-zero hex value in the CapAmb field
			stdoutRe: regexp.MustCompile(`CapAmb:\s+[0-9a-f]*[1-9a-f][0-9a-f]*`),
		},
		{
			name:           "cap_add ambient cap is raised in root task",
			user:           "root",
			command:        "sh",
			args:           []string{"-c", "grep CapAmb /proc/self/status"},
			unveilDefaults: true,
			unveilPaths:    []string{"r:/proc"},
			allowCaps:      []string{"net_bind_service"},
			capAdd:         []string{"net_bind_service"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`CapAmb:\s+[0-9a-f]*[1-9a-f][0-9a-f]*`),
		},
		// no cap_add means CapAmb must be all-zeroes; guards against accidental raises.
		{
			name:           "no cap_add means CapAmb is zero",
			user:           "nomad-80000",
			command:        "sh",
			args:           []string{"-c", "grep CapAmb /proc/self/status"},
			unveilDefaults: true,
			unveilPaths:    []string{"r:/proc"},
			allowCaps:      []string{"net_bind_service"},
			// capAdd intentionally omitted — no caps requested
			exp:      &drivers.ExitResult{ExitCode: 0},
			stdoutRe: regexp.MustCompile(`CapAmb:\s+0000000000000000`),
		},
		// cap names in mixed casing with CAP_ prefix must be accepted when
		// allow_caps holds the lowercase unprefixed equivalent.
		{
			name:           "cap_add normalized casing accepted",
			user:           "nomad-80000",
			command:        "sh",
			args:           []string{"-c", "grep CapAmb /proc/self/status"},
			unveilDefaults: true,
			unveilPaths:    []string{"r:/proc"},
			allowCaps:      []string{"net_bind_service"},     // lowercase, no prefix
			capAdd:         []string{"CAP_NET_BIND_SERVICE"}, // uppercase, with prefix
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`CapAmb:\s+[0-9a-f]*[1-9a-f][0-9a-f]*`),
		},
		// cap_drop removes a cap that cap_add granted; effective set must be empty.
		{
			name:           "cap_drop removes cap added by cap_add",
			user:           "nomad-80000",
			command:        "sh",
			args:           []string{"-c", "grep CapAmb /proc/self/status"},
			unveilDefaults: true,
			unveilPaths:    []string{"r:/proc"},
			allowCaps:      []string{"net_bind_service"},
			capAdd:         []string{"net_bind_service"},
			capDrop:        []string{"net_bind_service"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`CapAmb:\s+0000000000000000`),
    },
		// cwd is the task directory (not in a veiled parent path)
		{
			name:           "cwd is task dir",
			user:           "nomad-83000",
			command:        "sh",
			args:           []string{"-c", `test "$(pwd)" = "$NOMAD_TASK_DIR"`},
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		// work_dir inside sandbox — works without unveil_by_task because the
		// alloc dir is already unveiled by defaults
		{
			name:           "work_dir overrides cwd to alloc dir",
			user:           "nomad-84000",
			command:        "sh",
			args:           []string{"-c", `test "$(pwd)" = "$NOMAD_ALLOC_DIR"`},
			workDir:        "alloc", // resolves to <alloc>/alloc == NOMAD_ALLOC_DIR
			unveilDefaults: true,
			unveilByTask:   false, // no gate needed — inside sandbox
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		// work_dir inside sandbox — relative path to task dir, no gate needed
		{
			name:           "work_dir relative path resolved",
			user:           "nomad-86000",
			command:        "sh",
			args:           []string{"-c", `test "$(pwd)" = "$NOMAD_TASK_DIR"`},
			workDir:        "local", // resolves to <alloc>/<task>/local == NOMAD_TASK_DIR
			unveilDefaults: true,
			unveilByTask:   false, // no gate needed — inside sandbox
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		// /proc/self/mountinfo via explicit task unveil
		// convert() detects /proc/self/* via isProcSelfPath and promotes the entry to Dir("/proc","r")
		{
			name:           "read /proc/self/mountinfo via task unveil",
			user:           "nomad-87000",
			command:        "sh",
			args:           []string{"-c", "head -1 /proc/self/mountinfo"},
			unveilDefaults: true,
			unveilByTask:   true,
			unveil:         []string{"r:/proc/self/mountinfo"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`\d+ \d+ \d+:\d+`), // mountinfo line format
		},
		// /proc/cpuinfo via explicit task unveil
		// IsDir=false → File("/proc/cpuinfo","r") emitted directly.
		{
			name:           "read /proc/cpuinfo via task unveil",
			user:           "nomad-87000",
			command:        "sh",
			args:           []string{"-c", "head -1 /proc/cpuinfo"},
			unveilDefaults: true,
			unveilByTask:   true,
			unveil:         []string{"r:/proc/cpuinfo"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`.+`),
		},
		// Multiple specific /proc paths together — mirrors the exact DSE jobspec.
		{
			name:           "read multiple /proc paths via task unveil",
			user:           "nomad-87000",
			command:        "sh",
			args:           []string{"-c", "head -1 /proc/self/mountinfo && head -1 /proc/cpuinfo && head -1 /proc/meminfo"},
			unveilDefaults: true,
			unveilByTask:   true,
			unveil:         []string{"r:/proc/self/mountinfo", "r:/proc/cpuinfo", "r:/proc/meminfo"},
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		// /proc root via task unveil — directory form. os.Stat("/proc") succeeds
		// and IsDir=true so Dir("/proc","r") is emitted; all sub-paths accessible.
		{
			name:           "read /proc/self/mountinfo via /proc root unveil",
			user:           "nomad-87000",
			command:        "sh",
			args:           []string{"-c", "head -1 /proc/self/mountinfo && head -1 /proc/cpuinfo"},
			unveilDefaults: true,
			unveilByTask:   true,
			unveil:         []string{"r:/proc"},
			exp:            &drivers.ExitResult{ExitCode: 0},
			stdoutRe:       regexp.MustCompile(`\d+ \d+ \d+:\d+`),
		},
		// work_dir outside sandbox without unveil_by_task — must be rejected
		{
			name:           "work_dir outside sandbox rejected without unveil_by_task",
			user:           "nomad-85000",
			command:        "pwd",
			workDir:        "/tmp", // outside alloc dir — needs gate
			unveilByTask:   false,
			unveilDefaults: true,
			exp:            nil, // StartTask itself returns an error; no exit result
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pluginConfig := &Config{
				UnveilDefaults: tc.unveilDefaults,
				UnveilByTask:   tc.unveilByTask,
				UnveilPaths:    tc.unveilPaths,
				AllowCaps:      tc.allowCaps,
			}

			allocID := uuid.Generate()
			taskName := "test_cases_" + uuid.Short()
	
			task := &drivers.TaskConfig{
				User:      tc.user,
				ID:        uuid.Generate(),
				Name:      taskName,
				AllocID:   allocID,
				Resources: basicResources(allocID, taskName),
			}
	
			harness := newTestHarness(t, pluginConfig)
			harness.MakeTaskCgroup(task.AllocID, task.Name)
			cleanup := harness.MkAllocDir(task, true)
			defer cleanup()
	
			taskConfig := &TaskConfig{
				Command: tc.command,
				Args:    tc.args,
				Unveil:  tc.unveil,
				CapAdd:  tc.capAdd,
				CapDrop: tc.capDrop,
				WorkDir: tc.workDir,
			}

			must.NoError(t, task.EncodeConcreteDriverConfig(&taskConfig))

			// Start the task
			_, _, err := harness.StartTask(task)

			// cases with exp==nil expect StartTask itself to return an error
			if tc.exp == nil {
				must.Error(t, err)
				return
			}
			must.NoError(t, err)

			defer func() { _ = harness.DestroyTask(task.ID, true) }()

			// Attempt to wait
			waitCh, err := harness.WaitTask(context.Background(), task.ID)
			must.NoError(t, err)

			select {
			case result := <-waitCh:
				must.Eq(t, tc.exp, result, debugExitResult(result))
			case <-time.After(10 * time.Second):
				t.Fatalf("timeout")
			}

			// allow log collection to happen
			time.Sleep(3 * time.Second)

			// Assert logs contain expected outputs
			checkLogs(t, task, tc.stdoutRe, tc.stderrRe)
		})
	}
}

func checkLogs(t *testing.T, task *drivers.TaskConfig, outRe, errRe *regexp.Regexp) {
	if outRe == nil && errRe == nil {
		return
	}
	stdout, stderr := getLogs(t, task)
	if outRe != nil {
		must.RegexMatch(t, outRe, stdout)
	}
	if errRe != nil {
		must.RegexMatch(t, errRe, stderr)
	}
}

// getLogs will wait on the FIFO of the task to be flushed and return the
// standard out / standard error log content when available.
// It waits until both expected outputs are present to avoid returning before
// a slow write (e.g. when both stdoutRe and stderrRe are asserted).
func getLogs(t *testing.T, task *drivers.TaskConfig) (string, string) {
	outfile := filepath.Join(filepath.Dir(task.StdoutPath), fmt.Sprintf("%s.stdout.0", task.Name))
	errfile := filepath.Join(filepath.Dir(task.StderrPath), fmt.Sprintf("%s.stderr.0", task.Name))

	var stdout, stderr string
	for range 20 {
		outBytes, _ := os.ReadFile(outfile)
		stdout = string(bytes.TrimSpace(outBytes))

		errBytes, _ := os.ReadFile(errfile)
		stderr = string(bytes.TrimSpace(errBytes))

		if stdout != "" || stderr != "" {
			return stdout, stderr
		}

		time.Sleep(1 * time.Second)
	}

	t.Fatalf("no content in stdout and stderr logs (%s, %s)", outfile, errfile)
	return "", ""
}

func Test_doFingerprint_normal(t *testing.T) {
	ctests.RequireRoot(t)

	p := new(Plugin)
	p.config = &Config{
		UnveilByTask:   true,
		UnveilDefaults: true,
		AllowCaps:      []string{"net_bind_service", "chown"},
	}
	fp := p.doFingerprint(exec.LookPath)

	must.Eq(t, drivers.HealthStateHealthy, fp.Health)
	must.Eq(t, drivers.DriverHealthy, fp.HealthDescription)
	must.Eq(t, map[string]*dstructs.Attribute{
		"driver.exec2.unveil.tasks":    dstructs.NewBoolAttribute(true),
		"driver.exec2.unveil.defaults": dstructs.NewBoolAttribute(true),
		"driver.exec2.caps.allowlist":  dstructs.NewStringAttribute("net_bind_service,chown"),
	}, fp.Attributes)
}

func Test_doFingerprint_notRoot(t *testing.T) {
	ctests.RequireNonRoot(t)

	p := new(Plugin)
	fp := p.doFingerprint(nil)

	must.Eq(t, drivers.HealthStateUndetected, fp.Health)
	must.Eq(t, drivers.DriverRequiresRootMessage, fp.HealthDescription)
}

func Test_doFingerprint_missing_nsenter(t *testing.T) {
	ctests.RequireRoot(t)

	nsenterLookupFailure := func(name string) (string, error) {
		if name == "nsenter" {
			return "", os.ErrNotExist
		}
		return filepath.Join("/bin", name), nil
	}

	p := new(Plugin)
	fp := p.doFingerprint(nsenterLookupFailure)

	must.Eq(t, drivers.HealthStateUndetected, fp.Health)
	must.Eq(t, "nsenter executable not found", fp.HealthDescription)
}

func Test_doFingerprint_missing_unshare(t *testing.T) {
	ctests.RequireRoot(t)

	unshareLookupFailure := func(name string) (string, error) {
		if name == "unshare" {
			return "", os.ErrNotExist
		}
		return filepath.Join("/bin", name), nil
	}

	p := new(Plugin)
	fp := p.doFingerprint(unshareLookupFailure)

	must.Eq(t, drivers.HealthStateUndetected, fp.Health)
	must.Eq(t, "unshare executable not found", fp.HealthDescription)
}

func Test_tools(t *testing.T) {
	t.Run("unshare", func(t *testing.T) {
		path, err := exec.LookPath("unshare")
		must.NoError(t, err)
		t.Log("path to unshare is: " + path)
	})

	t.Run("nsenter", func(t *testing.T) {
		path, err := exec.LookPath("nsenter")
		must.NoError(t, err)
		t.Log("path to nsenter is: " + path)
	})
}

// TestSetConfig_AllowCaps_Validation verifies that SetConfig rejects unknown
// capability names in the allow_caps plugin configuration.
func TestSetConfig_AllowCaps_Validation(t *testing.T) {
	ci.Parallel(t)

	logger := testlog.HCLogger(t)
	p := New(logger).(*Plugin)

	baseConfig := &base.Config{
		AgentConfig: &base.AgentConfig{
			Driver: &base.ClientDriverConfig{
				Topology: structs.MockWorkstationTopology(),
			},
		},
	}

	invalidConfig := &Config{
		UnveilDefaults: true,
		AllowCaps:      []string{"net_bind_service", "not_a_real_capability"},
	}
	must.NoError(t, base.MsgPackEncode(&baseConfig.PluginConfig, invalidConfig))

	err := p.SetConfig(baseConfig)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "not_a_real_capability")
}

// TestFunctional_cap_add_not_allowed verifies that a task requesting a
// capability not present in the plugin allow_caps list is rejected at start
// time with a descriptive error.
func TestFunctional_cap_add_not_allowed(t *testing.T) {
	ctests.RequireRoot(t)
	ci.Parallel(t)

	pluginConfig := &Config{
		UnveilDefaults: true,
		AllowCaps:      []string{}, // empty: no caps permitted
	}

	taskConfig := &TaskConfig{
		Command: "env",
		CapAdd:  []string{"net_bind_service"},
	}

	allocID := uuid.Generate()
	taskName := "test_caps_not_allowed_" + uuid.Short()

	task := &drivers.TaskConfig{
		User:      "nomad-80000",
		ID:        uuid.Generate(),
		Name:      taskName,
		AllocID:   allocID,
		Resources: basicResources(allocID, taskName),
	}
	must.NoError(t, task.EncodeConcreteDriverConfig(&taskConfig))

	harness := newTestHarness(t, pluginConfig)
	harness.MakeTaskCgroup(task.AllocID, task.Name)
	cleanup := harness.MkAllocDir(task, true)
	defer cleanup()

	_, _, err := harness.StartTask(task)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "net_bind_service")
}

// TestFunctional_TaskStats_RSS verifies that RSS is reported as a non-zero value
// in the MemoryStats of a running task, and that "RSS" is included in Measured.
// RSS is derived from the "anon" field of the cgroup memory.stat file.
func TestFunctional_TaskStats_RSS(t *testing.T) {
	ctests.RequireRoot(t)
	ci.Parallel(t)

	pluginConfig := &Config{
		UnveilDefaults: true,
	}

	taskConfig := &TaskConfig{
		Command: "sleep",
		Args:    []string{"infinity"},
	}

	allocID := uuid.Generate()
	taskName := "test_rss_stats_" + uuid.Short()

	task := &drivers.TaskConfig{
		User:      "nomad-80000",
		ID:        uuid.Generate(),
		Name:      taskName,
		AllocID:   allocID,
		Resources: basicResources(allocID, taskName),
	}

	must.NoError(t, task.EncodeConcreteDriverConfig(&taskConfig))

	harness := newTestHarness(t, pluginConfig)
	harness.MakeTaskCgroup(task.AllocID, task.Name)
	t.Cleanup(harness.MkAllocDir(task, true))

	_, _, err := harness.StartTask(task)
	must.NoError(t, err)

	t.Cleanup(func() {
		_ = harness.DestroyTask(task.ID, true)
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// TaskStats emits on the given interval; the first emission populates cgroup
	// memory.stat values. 200ms is fast enough to not slow the test suite.
	statsCh, err := harness.TaskStats(ctx, task.ID, 200*time.Millisecond)
	must.NoError(t, err)

	select {
	case usage := <-statsCh:
		must.NotNil(t, usage)
		must.NotNil(t, usage.ResourceUsage)

		mem := usage.ResourceUsage.MemoryStats
		must.NotNil(t, mem)

		// RSS must be non-zero — a running process always has anonymous memory
		// (stack at minimum). It is sourced from the "anon" field in memory.stat.
		must.Positive(t, mem.RSS)

		// "RSS" must be declared in Measured so Nomad surfaces it in the UI/API
		must.SliceContainsAll(t, mem.Measured, []string{"RSS", "Cache", "Swap", "Usage"})

	case <-ctx.Done():
		t.Fatal("timed out waiting for task stats")
	}
}
