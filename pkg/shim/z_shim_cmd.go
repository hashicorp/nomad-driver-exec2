// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"

	"github.com/hashicorp/nomad-driver-exec2/pkg/util"
	"github.com/hashicorp/nomad/helper/subproc"
)

const (
	// SubCommand is the first argument to the clone of the nomad agent process
	// for invoking the exec2 driver sandbox shim.
	SubCommand = "exec2-shim"

	// ExitWrongArgs indicates the shim has terminated early due to recieving
	// the wrong expected arguments. We use a special return code here since logs
	// will not have been configured yet.
	ExitWrongArgs = 40

	// ExitBadLogging indicates the shim has terminated early due to being unable
	// to open stdout or stderr output files (fifos).
	ExitBadLogging = 41
)

// init is the entrypoint for the 'nomad e2e-shim' invocation of nomad
//
// The argument format is as follows,
//
// 0. nomad            <- the executable name
// 1. exec2-shim       <- this subcommand
// 2. true/false       <- include default unveil paths
// 3. <stdout path>    <- path to named pipe for standard output
// 4. <stderr path>    <- path to named pipe for standard error
// 5. [mode:path, ...] <- list of additional unveil paths
// 6. --               <- sentinel between following commands
func init() {
	subproc.Do(SubCommand, func() int {
		// we need to ignore the stop signal (which is sent to the entire
		// process group) so that we stay alive and can capture the exit code
		// of the child task process
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs)
		go func() {
			<-sigs // do nothing; say alive
		}()

		if n := len(os.Args); n <= 4 {
			subproc.Print("failed to invoke e2e-shim with sufficient args: %d", n)
			return ExitWrongArgs
		}

		// get the unveil paths and the rest of the command(s) to run
		// from our command arguments
		args := os.Args[5:] // chop off 'nomad exec2-shim <defaults>'
		defaults := os.Args[2] == "true"
		outPipePath := os.Args[3]
		errPipePath := os.Args[4]
		paths, commands := split(args)
		paths = append(paths, "w:"+outPipePath)
		paths = append(paths, "w:"+errPipePath)

		stdout, stderr, err := util.OpenPipes(outPipePath, errPipePath)
		if err != nil {
			subproc.Print("failed to open output pipes: %v", err)
			return ExitBadLogging
		}

		// give ourselves a way to write to the stderr pipe for printing fatal errors
		debug := func(format string, args ...any) {
			_, _ = io.WriteString(stderr, fmt.Sprintf(format, args...))
		}

		// use landlock to isolate this process and child processes to the
		// set of given filepaths
		if err := lockdown(defaults, paths); err != nil {
			debug("unable to lockdown: %v", err)
			return subproc.ExitFailure
		}

		// locate the absolute path for the task command, as this must be
		// the first argument to the execve(2) call that follows
		cmdpath, err := exec.LookPath(commands[0])
		if err != nil {
			debug("failed to locate command %q: %v", commands[0], err)
			return subproc.ExitNotRunnable
		}

		// invoke the task command with its args
		// the environment has already been set for us by the exec2 driver
		cmd := exec.Command(cmdpath, commands[1:]...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr

		var code = 0
		if err = cmd.Run(); err != nil {
			ee := err.(*exec.ExitError)
			code = ee.ProcessState.ExitCode()
		}

		_ = stdout.Close()
		_ = stderr.Close()

		// retrieve the exit status of the task process and write it to a
		// known location in case the plugin driver needs to read it back
		destination := filepath.Join(os.Getenv("NOMAD_TASK_DIR"), ".exit_status.txt")
		_ = os.WriteFile(destination, []byte(strconv.Itoa(code)), 0o644)
		return code
	})
}
