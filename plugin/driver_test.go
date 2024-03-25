// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

	baseConfig := new(base.Config)

	// encode and set plugin config
	must.NoError(t, base.MsgPackEncode(&baseConfig.PluginConfig, pluginConfig))
	must.NoError(t, plugin.SetConfig(baseConfig))

	// set initial fingerprint
	plugin.doFingerprint(exec.LookPath)

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

func TestRun_basics(t *testing.T) {
	ci.Parallel(t)

	// note: all tasks must be batch and complete in under 10 seconds

	cases := []struct {
		name string

		// task config
		user    string
		command string
		args    []string
		unveil  []string

		// plugin config
		unveilDefaults bool
		unveilByTask   bool
		unveilPaths    []string

		// expectations
		exp *drivers.ExitResult
	}{
		// run 'env' with default unveil paths
		{
			name:           "dynamic user",
			user:           "nomad-80000",
			command:        "env",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		{
			name:           "nobody user",
			user:           "nobody",
			command:        "env",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		{
			name:           "root user",
			user:           "root",
			command:        "env",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 0},
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
		},
		{
			name:           "read /etc/passwd as nobody using defaults",
			user:           "nobody",
			command:        "cat",
			unveilDefaults: true,
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 1},
		},
		{
			name:           "read /etc/passwd as root using defaults",
			user:           "root",
			command:        "cat",
			unveilDefaults: true,
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 1},
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
		},
		{
			name:           "read /etc/passwd as nobody using custom paths via plugin",
			user:           "nobody",
			command:        "cat",
			unveilDefaults: true,
			unveilPaths:    []string{"r:/etc/passwd"},
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		{
			name:           "read /etc/passwd as root using custom paths via plugin",
			user:           "root",
			command:        "cat",
			unveilDefaults: true,
			unveilPaths:    []string{"r:/etc/passwd"},
			args:           []string{"/etc/passwd"},
			exp:            &drivers.ExitResult{ExitCode: 0},
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
		},
		// try to execute a non-existent file
		{
			name:           "execute non-existent program",
			user:           "nomad-80000",
			command:        "/usr/bin/doesnotexist",
			unveilDefaults: true,
			exp:            &drivers.ExitResult{ExitCode: 127},
		},
		// try to execute non-executable file
		{
			name:           "execute non-executable file",
			user:           "nomad-80000",
			command:        "/etc/os-release",
			unveilDefaults: true,
			unveilPaths:    []string{"rx:/etc"},
			exp:            &drivers.ExitResult{ExitCode: 127},
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
			command:        "cp",
			unveilDefaults: true,
			args:           []string{"/etc/hosts", "${NOMAD_TASK_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		{
			name:           "write to alloc directory",
			user:           "nomad-80000",
			command:        "cp",
			unveilDefaults: true,
			args:           []string{"/etc/hosts", "${NOMAD_ALLOC_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
		{
			name:           "write to secrets directory",
			user:           "nomad-80000",
			command:        "cp",
			unveilDefaults: true,
			args:           []string{"/etc/hosts", "${NOMAD_SECRETS_DIR}"},
			exp:            &drivers.ExitResult{ExitCode: 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pluginConfig := &Config{
				UnveilDefaults: tc.unveilDefaults,
				UnveilByTask:   tc.unveilByTask,
				UnveilPaths:    tc.unveilPaths,
			}

			taskConfig := &TaskConfig{
				Command: tc.command,
				Args:    tc.args,
				Unveil:  tc.unveil,
			}

			allocID := uuid.Generate()
			taskName := "basics_test_" + uuid.Short()

			task := &drivers.TaskConfig{
				User:      tc.user,
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
		})
	}
}

func Test_doFingerprint_normal(t *testing.T) {
	ctests.RequireRoot(t)

	p := new(Plugin)
	p.config = &Config{
		UnveilByTask:   true,
		UnveilDefaults: true,
	}
	fp := p.doFingerprint(exec.LookPath)

	must.Eq(t, drivers.HealthStateHealthy, fp.Health)
	must.Eq(t, drivers.DriverHealthy, fp.HealthDescription)
	must.Eq(t, map[string]*dstructs.Attribute{
		"driver.exec2.unveil.tasks":    dstructs.NewBoolAttribute(true),
		"driver.exec2.unveil.defaults": dstructs.NewBoolAttribute(true),
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
