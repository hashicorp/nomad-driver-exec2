// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/hashicorp/nomad-driver-exec2/pkg/capabilities"
	"github.com/hashicorp/nomad-driver-exec2/pkg/util"
	"github.com/hashicorp/nomad/helper/subproc"
)

// capHdr and capData mirror the kernel's __user_cap_header_struct and
// __user_cap_data_struct used by capset(2). Version 3 (0x20080522) supports
// capabilities 0–63 via two 32-bit words.
type capHdr struct {
	version uint32
	pid     int32
}

type capData struct {
	effective   uint32
	permitted   uint32
	inheritable uint32
}

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

	// ExitBadConfig indicates the shim has terminated early due to being unable
	// to read or parse the shim config file.
	ExitBadConfig = 42
)

// init is the entrypoint for the 'nomad exec2-shim' invocation of nomad
//
// The argument format is as follows,
//
//	0. nomad              <- the executable name
//	1. exec2-shim         <- this subcommand
//	2. <config file path> <- path to the JSON ShimConfig written by the driver
func init() {
	subproc.Do(SubCommand, func() int {
		// we need to ignore the stop signal (which is sent to the entire
		// process group) so that we stay alive and can capture the exit code
		// of the child task process
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs)
		go func() {
			<-sigs // do nothing; stay alive
		}()

		if len(os.Args) != 3 {
			subproc.Print("exec2-shim requires exactly 1 argument (config path), got %d", len(os.Args)-2)
			return ExitWrongArgs
		}

		cfg, err := ReadShimConfig(os.Args[2])
		if err != nil {
			subproc.Print("failed to read shim config %q: %v", os.Args[2], err)
			return ExitBadConfig
		}

		outPipePath := cfg.OutPipe
		errPipePath := cfg.ErrPipe

		// copy unveil paths into a new slice to avoid mutating cfg.UnveilPaths,
		// then append the write entries for stdout/stderr pipes
		paths := make([]string, len(cfg.UnveilPaths), len(cfg.UnveilPaths)+2)
		copy(paths, cfg.UnveilPaths)
		paths = append(paths, "w:"+outPipePath, "w:"+errPipePath)

		// resolve capability names to kernel integers before calling dropPrivileges;
		// this is a pure string-to-integer mapping that requires no privileges
		var caps []uintptr
		if len(cfg.Capabilities) > 0 {
			var capErr error
			caps, capErr = capabilities.Resolve(cfg.Capabilities)
			if capErr != nil {
				subproc.Print("failed to resolve capabilities: %v", capErr)
				return ExitBadConfig
			}
		}

		// drop privileges conditionally: for non-root tasks, transitions uid/gid;
		// for tasks with caps, restores them in permitted/effective/inheritable and
		// raises as ambient so they survive into the task process via exec().
		if err := dropPrivileges(cfg.UID, cfg.GID, caps); err != nil {
			subproc.Print("failed to drop privileges: %v", err)
			return subproc.ExitFailure
		}

		stdout, stderr, err := util.OpenPipes(outPipePath, errPipePath)
		if err != nil {
			subproc.Print("failed to open output pipes: %v", err)
			return ExitBadLogging
		}

		// give ourselves a way to write to the stderr pipe for printing fatal errors
		debug := func(format string, args ...any) {
			_, _ = io.WriteString(stderr, fmt.Sprintf(format+"\n", args...))
		}

		// use landlock to isolate this process and child processes to the
		// set of given filepaths
		if err := lockdown(cfg.UnveilDefaults, paths); err != nil {
			debug("unable to lockdown: %v", err)
			return subproc.ExitFailure
		}

		// locate the absolute path for the task command, as this must be
		// the first argument to the execve(2) call that follows
		cmdpath, err := exec.LookPath(cfg.Command)
		if err != nil {
			debug("failed to locate command %q: %v", cfg.Command, err)
			return subproc.ExitNotRunnable
		}

		// invoke the task command with its args;
		// ambient capabilities are already raised in this process and will be
		// inherited by the child across the exec() boundary automatically
		
		// the environment has already been set for us by the exec2 driver;
		// NOMAD_WORK_DIR is set to work_dir if configured, otherwise NOMAD_TASK_DIR
		cmd := exec.Command(cmdpath, cfg.Arguments...)
		cmd.Dir = os.Getenv("NOMAD_WORK_DIR")
		cmd.Stdout = stdout
		cmd.Stderr = stderr

		var code = 0
		if err = cmd.Run(); err != nil {
			ee := err.(*exec.ExitError)
			code = ee.ExitCode()
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

// dropPrivileges conditionally drops uid/gid and adds the given capabilities
// to the ambient set of the shim process so they can be passed to the task process.
//
// This collapses the four cases into one function:
//
//	uid=0,  caps=[]  → 0 syscalls  (nothing to do)
//	uid=0,  caps=[…] → steps 4–6  (NO_NEW_PRIVS + CAPSET + AMBIENT_RAISE)
//	uid≠0,  caps=[]  → steps 2–3  (setresgid + setresuid only)
//	uid≠0,  caps=[…] → steps 1–7  (full sequence; KEEPCAPS preserves permitted across setresuid)
func dropPrivileges(uid, gid int, caps []uintptr) error {
	// All syscalls below are per-thread on Linux (setresuid, setresgid, capset,
	// prctl). Lock this goroutine to its OS thread so the Go scheduler cannot
	// migrate it between syscalls, which would cause them to execute on
	// different threads and silently corrupt capability state.
	runtime.LockOSThread()

	needDrop := uid != 0      // uid/gid transition only needed for non-root tasks
	needCaps := len(caps) > 0 // cap management only needed when caps are requested

	// step 1: keep permitted caps across the uid transition; only needed when
	// both a uid drop AND capability raising are required — if we stay root,
	// permitted is already full; if no caps are requested, KEEPCAPS is irrelevant
	if needDrop && needCaps {
		if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, unix.PR_SET_KEEPCAPS, 1, 0); errno != 0 {
			return fmt.Errorf("prctl PR_SET_KEEPCAPS=1: %w", errno)
		}
	}

	// steps 2+3: drop group and user id; only needed for non-root tasks
	if needDrop {
		// group must be dropped before uid (dropping uid first would lose CAP_SETGID)
		if _, _, errno := syscall.RawSyscall(syscall.SYS_SETRESGID, uintptr(gid), uintptr(gid), uintptr(gid)); errno != 0 {
			return fmt.Errorf("setresgid %d: %w", gid, errno)
		}
		if _, _, errno := syscall.RawSyscall(syscall.SYS_SETRESUID, uintptr(uid), uintptr(uid), uintptr(uid)); errno != 0 {
			return fmt.Errorf("setresuid %d: %w", uid, errno)
		}
	}

	// steps 4–6: manage capabilities when any were requested.
	if needCaps {
		// step 4: prevent privilege re-escalation via setuid binaries or file
		// capabilities on exec; sufficient on its own to block all exec-time
		// escalation vectors, making bounding-set restriction unnecessary.
		if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); errno != 0 {
			return fmt.Errorf("prctl PR_SET_NO_NEW_PRIVS: %w", errno)
		}

		// step 5: restore caps in permitted/effective/inheritable then raise as
		// ambient so they survive into the task process via exec().
		// For root the permitted set is already full so CAPSET works without KEEPCAPS.
		hdr := capHdr{version: 0x20080522} // _LINUX_CAPABILITY_VERSION_3
		data := [2]capData{}
		for _, c := range caps {
			idx := c / 32
			mask := uint32(1) << (c % 32)
			data[idx].effective |= mask
			data[idx].permitted |= mask
			data[idx].inheritable |= mask
		}
		if _, _, errno := syscall.RawSyscall(syscall.SYS_CAPSET,
			uintptr(unsafe.Pointer(&hdr)),
			uintptr(unsafe.Pointer(&data[0])),
			0,
		); errno != 0 {
			return fmt.Errorf("capset: %w", errno)
		}

		// step 6: raise each cap as ambient so it is inherited across exec().
		for _, c := range caps {
			if _, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL,
				unix.PR_CAP_AMBIENT,
				unix.PR_CAP_AMBIENT_RAISE,
				c, 0, 0, 0,
			); errno != 0 {
				return fmt.Errorf("prctl PR_CAP_AMBIENT_RAISE %d: %w", c, errno)
			}
		}
	}

	// step 7: clear KEEPCAPS — mirrors step 1; only set when needDrop && needCaps
	if needDrop && needCaps {
		if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, unix.PR_SET_KEEPCAPS, 0, 0); errno != 0 {
			return fmt.Errorf("prctl PR_SET_KEEPCAPS=0: %w", errno)
		}
	}

	return nil
}
