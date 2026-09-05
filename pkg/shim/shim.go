// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-set/v2"
	"github.com/hashicorp/nomad-driver-exec2/pkg/resources"
	"github.com/hashicorp/nomad-driver-exec2/pkg/resources/process"
	"github.com/hashicorp/nomad/helper/users/dynamic"
	"github.com/hashicorp/nomad/plugins/drivers"
	"golang.org/x/sys/unix"
)

// Options represent Task configuration options.
type Options struct {
	Command        string
	Arguments      []string
	UnveilPaths    []string
	UnveilDefaults bool
	OOMScoreAdj    int
	Capabilities   []string
	WorkDir        string // working directory for the task; defaults to TaskDir
}

// Environment represents runtime configuration.
type Environment struct {
	User         string            // user the command will run as (may be empty / synthetic)
	OutPipe      string            // io pipe path for stdout
	ErrPipe      string            // io pipe path for stderr
	Env          map[string]string // environment variables
	TaskDir      string            // task directory
	WorkDir      string            // working directory for the task; defaults to TaskDir
	Cgroup       string            // task cgroup path
	Net          string            // allocation network namespace path
	Memory       uint64            // memory in megabytes
	MemoryMax    uint64            // memory_max in megabytes
	CPUBandwidth uint64            // cpu / cores bandwidth
	OOMScoreAdj  int               // oom_score_adj for the task
}

type ExecTwo interface {
	// Start the Task process.
	Start(context.Context) error

	// PID returns the process ID associated with exec.
	//
	// Must only be called after Start.
	PID() int

	// Wait on the process (until exit).
	//
	// Must only be called after Start.
	WaitCh() process.WaitCh

	// Stats returns current resource utilization.
	//
	// Must only be called after Start.
	Stats() *resources.Utilization

	// Signal [kill()] the process.
	//
	// Must be called after Start.
	Signal(string) error

	// Stop the process.
	//
	// Must be called after Start.
	Stop(string, time.Duration) error
}

// New an ExecTwo, an instantiation of the exec2 driver.
func New(env *Environment, opts *Options, l hclog.Logger) ExecTwo {
	return &exe{
		env:    env,
		opts:   opts,
		cpu:    new(resources.TrackCPU),
		logger: l,
	}
}

// Recover an ExecTwo, an already running instance of the execc2 driver.
func Recover(pid int, env *Environment, l hclog.Logger) ExecTwo {
	return &exe{
		pid:     pid,
		env:     env,
		opts:    nil, // already started, not used now
		waiter:  process.WaitPID(pid, env.TaskDir).Wait(),
		signals: process.Signals(pid),
		cpu:     new(resources.TrackCPU),
		logger:  l,
	}
}

type exe struct {
	// comes from task config
	env  *Environment
	opts *Options

	// comes from runtime
	pid     int
	cpu     *resources.TrackCPU
	waiter  process.WaitCh
	signals process.Signaler

	// pipe file descriptors opened for the wrapper process (nsenter/unshare);
	// opened in prepare() and closed once the process exits
	outfd *os.File
	errfd *os.File

	// comes from New/Recover
	logger hclog.Logger
}

func (e *exe) Start(ctx context.Context) error {
	uid, gid, home, err := dynamic.LookupUser(e.env.User)
	if err != nil {
		return fmt.Errorf("failed to lookup user: %w", err)
	}

	// find out cgroup file descriptor
	fd, cleanup, err := e.openCG()
	if err != nil {
		return fmt.Errorf("failed to open cgroup for descriptor: %w", err)
	}

	// close the cgroup descriptor after start or failure
	defer cleanup()

	// set resource constraints
	if err = e.constrain(); err != nil {
		return fmt.Errorf("failed to write cgroup constraints: %w", err)
	}

	// set permissions on fifos for logging output
	if err = e.fixPipes(uid, gid); err != nil {
		return fmt.Errorf("failed to set logging pipe ownership: %w", err)
	}

	// create sandbox using nsenter, unshare, and our cgroup
	cmd, err := e.prepare(ctx, home, fd, uid, gid)
	if err != nil {
		return err
	}

	// prepare() opened the pipe FDs, closing them if we return before the
	// waiter goroutine is launched, covers any subsequent error before e.waiter is set.
	defer func() {
		if e.waiter == nil {
			_ = e.outfd.Close()
			_ = e.errfd.Close()
		}
	}()

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// set oom_score_adj
	if e.env.OOMScoreAdj > 0 {
		if err = e.setOomScoreAdj(cmd.Process.Pid); err != nil {
			return fmt.Errorf("failed to set oom score adj: %w", err)
		}
	}

	// attach to the underlying unix process
	e.pid = cmd.Process.Pid
	e.signals = process.Signals(cmd.Process.Pid)

	// close the pipe file descriptors after the process exits
	inner := process.WaitProc(cmd.Process).Wait()
	waiter := make(chan *drivers.ExitResult, 1)
	go func() {
		result := <-inner
		_ = e.outfd.Close()
		_ = e.errfd.Close()
		waiter <- result
	}()
	e.waiter = waiter

	return nil
}

func (e *exe) fixPipes(uid, gid int) error {
	if err := fixpipe(e.env.OutPipe, uid, gid); err != nil {
		return err
	}
	if err := fixpipe(e.env.ErrPipe, uid, gid); err != nil {
		return err
	}
	return nil
}

func fixpipe(path string, uid, gid int) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// After a host reboot the alloc-mounts bind mount is gone; recreate the
	// logs directory and FIFO before attempting to chown either of them.
	// Use 0o777 to match the permissions Nomad sets when it creates the logs
	// directory in allocdir (SharedAllocDirs are created with fileMode777).
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("error creating fifo parent directory %q: %w", dir, err)
	}
	if err := unix.Mkfifo(path, 0o600); err != nil && !os.IsExist(err) {
		return fmt.Errorf("error creating fifo %q: %w", path, err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("error opening fifo parent directory %q: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	return root.Chown(base, uid, gid)
}

func (e *exe) PID() int {
	return e.pid
}

func (e *exe) WaitCh() process.WaitCh {
	return e.waiter
}

func (e *exe) Signal(s string) error {
	return e.signals.Send(s)
}

func (e *exe) Stop(signal string, timeout time.Duration) error {
	// politely ask the group to terminate via user specified signal
	err := e.Signal(signal)
	if e.blockPIDs(timeout) {
		// no more mr. nice guy, kill the whole cgroup
		_ = e.writeCG("cgroup.kill", "1")
	}
	return err
}

func (e *exe) Stats() *resources.Utilization {
	memCurrentS, _ := e.readCG("memory.current")
	memCurrent, _ := strconv.Atoi(memCurrentS)

	swapCurrentS, _ := e.readCG("memory.swap.current")
	swapCurrent, _ := strconv.Atoi(swapCurrentS)

	memStatS, _ := e.readCG("memory.stat")
	memCache, memRSS := extractMemStat(memStatS)

	cpuStatsS, _ := e.readCG("cpu.stat")
	usr, system, total := extractCPU(cpuStatsS)
	userPct, systemPct, totalPct := e.cpu.Percent(usr, system, total)

	specs := resources.GetSpecs()
	ticks := (.01 * totalPct) * resources.Percent(int(specs.Ticks())/specs.Cores)

	return &resources.Utilization{
		// memory stats
		Memory: uint64(memCurrent),
		Swap:   uint64(swapCurrent),
		Cache:  memCache,
		RSS:    memRSS,

		// cpu stats
		System:  systemPct,
		User:    userPct,
		Percent: totalPct,
		Ticks:   ticks,
	}
}

func (e *exe) openCG() (int, func(), error) {
	fd, err := unix.Open(e.env.Cgroup, unix.O_PATH, 0)
	cleanup := func() { _ = unix.Close(fd) }
	return fd, cleanup, err
}

func (e *exe) readCG(file string) (string, error) {
	file = filepath.Join(e.env.Cgroup, file)
	b, err := os.ReadFile(file)
	return strings.TrimSpace(string(b)), err
}

func (e *exe) writeCG(file, content string) error {
	file = filepath.Join(e.env.Cgroup, file)
	f, err := os.OpenFile(file, os.O_WRONLY, 0o700)
	if err != nil {
		return fmt.Errorf("failed to open cgroup file: %w", err)
	}
	if _, err = io.Copy(f, strings.NewReader(content)); err != nil {
		return fmt.Errorf("failed to write pid to cgroup file: %w", err)
	}

	return f.Close()
}

func flatten(user, home, workDir string, env map[string]string) []string {
	result := make([]string, 0, len(env))

	// override and remove some variables
	ignoredEnv := set.From([]string{
		// remove useless envs
		"LS_COLORS",
		"XAUTHORITY",
		"DISPLAY",
		"COLORTERM",
		"MAIL",
	})

	env["USER"] = user
	env["HOME"] = home

	// set the tmp directory to the one made for the task
	parent := filepath.Dir(env["NOMAD_TASK_DIR"])
	tmp := filepath.Join(parent, "tmp")
	env["TMPDIR"] = tmp

	// set the working directory; defaults to NOMAD_TASK_DIR when not overridden
	if workDir != "" {
		env["NOMAD_WORK_DIR"] = workDir
	} else {
		env["NOMAD_WORK_DIR"] = env["NOMAD_TASK_DIR"]
	}

	// copy environment variables into list form
	for k, v := range env {
		switch {
		case ignoredEnv.Contains(k) || strings.HasPrefix(k, "LD_"):
			continue // skip setting ignored or dynamic linker variables
		case v == "":
			result = append(result, k)
		default:
			result = append(result, k+"="+v)
		}
	}

	return result
}

func self() string {
	executable, err := os.Executable()
	if err != nil {
		msg := fmt.Sprintf("plugin: unable to find executable: %v", err)
		panic(msg)
	}

	// when running unit tests we must fix the grandparent of the output
	// executable to allow execution as other users
	if strings.HasSuffix(executable, ".test") {
		parent := filepath.Dir(executable)
		gparent := filepath.Dir(parent)
		if err = os.Chmod(gparent, 0755); err != nil {
			msg := fmt.Sprintf("plugin: unable to chmod: %v", err)
			panic(msg)
		}
	}

	return executable
}

func (e *exe) parameters(uid, gid int) []string {
	var result []string

	// setup nsenter if task was assigned a network namespace
	if net := e.env.Net; net != "" {
		result = append(
			result,
			"nsenter",
			"--no-fork",
			fmt.Sprintf("--net=%s", net),
			"--",
		)
	}

	// setup unshare for ipc, pid namespaces, mount propagation; uid/gid transition is handled
	// by the shim itself (after the fork) so it can manage capabilities correctly
	result = append(result,
		"unshare",
		"--ipc",
		"--pid",
		"--mount-proc",
		"--propagation",
		"slave",
		"--fork",
		"--kill-child=SIGKILL",
		"--",
	)

	// setup ourself '$0 exec2-shim' for unveil
	result = append(result, self(), SubCommand)
	result = append(result, strconv.FormatBool(e.opts.UnveilDefaults))
	result = append(result, e.env.OutPipe)
	result = append(result, e.env.ErrPipe)
	// pass uid and gid so the shim can drop privileges itself (after fork,
	// before exec) with PR_SET_KEEPCAPS to preserve the ambient capability set
	result = append(result, strconv.Itoa(uid))
	result = append(result, strconv.Itoa(gid))
	// pass capability names as a comma-separated string; empty means no caps
	result = append(result, strings.Join(e.opts.Capabilities, ","))
	result = append(result, e.opts.UnveilPaths...)
	result = append(result, "--")

	// append the user command
	result = append(result, e.opts.Command)
	if len(e.opts.Arguments) > 0 {
		result = append(result, e.opts.Arguments...)
	}

	// craft complete result
	return result
}

// create an exec.Cmd to run our process tree
func (e *exe) prepare(ctx context.Context, home string, fd, uid, gid int) (*exec.Cmd, error) {
	params := e.parameters(uid, gid)
	cmd := exec.CommandContext(ctx, params[0], params[1:]...)

	// Open the pipes via os.Root so that the open is resolved relative to
	// the alloc logs directory and cannot be redirected outside it by a
	// symlink swap attack across tasks or restarted tasks.
	pipeDir := filepath.Dir(e.env.OutPipe)
	root, err := os.OpenRoot(pipeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open pipe directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	outfd, err := root.OpenFile(filepath.Base(e.env.OutPipe), os.O_WRONLY, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to open stdout pipe: %w", err)
	}
	cmd.Stdout = outfd
	e.outfd = outfd

	errfd, err := root.OpenFile(filepath.Base(e.env.ErrPipe), os.O_WRONLY, 0700)
	if err != nil {
		_ = outfd.Close()
		return nil, fmt.Errorf("failed to open stderr pipe: %w", err)
	}
	cmd.Stderr = errfd
	e.errfd = errfd

	cmd.Env = flatten(e.env.User, home, e.env.WorkDir, e.env.Env)
	// use work_dir when set, otherwise fall back to the task directory
	if e.env.WorkDir != "" {
		cmd.Dir = e.env.WorkDir
	} else {
		cmd.Dir = e.env.TaskDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		UseCgroupFD: true, // clone directly into cgroup
		CgroupFD:    fd,   // cgroup file descriptor
		Setpgid:     true, // ignore signals sent to nomad
	}

	return cmd, nil
}

// set resource constraints via cgroups
func (e *exe) constrain() error {
	// set cpu bandwidth
	if err := e.writeCG("cpu.max", fmt.Sprintf("%d 100000", e.env.CPUBandwidth)); err != nil {
		return err
	}

	// set memory limits
	switch e.env.MemoryMax {
	case 0:
		if err := e.writeCG("memory.max", fmt.Sprintf("%d", e.env.Memory)); err != nil {
			return err
		}
	default:
		if err := e.writeCG("memory.low", fmt.Sprintf("%d", e.env.Memory)); err != nil {
			return err
		}
		if err := e.writeCG("memory.max", fmt.Sprintf("%d", e.env.MemoryMax)); err != nil {
			return err
		}
	}
	return nil
}

func (e *exe) setOomScoreAdj(pid int) error {
	return os.WriteFile(
		fmt.Sprintf("/proc/%d/oom_score_adj", pid),
		[]byte(strconv.Itoa(int(e.env.OOMScoreAdj))),
		0644,
	)
}

func extractMemStat(s string) (cache, rss uint64) {
	read := func(line string) uint64 {
		num := line[strings.Index(line, " ")+1:]
		v, _ := strconv.ParseUint(num, 10, 64)
		return v
	}
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		text := scanner.Text()
		switch {
		case strings.HasPrefix(text, "file "):
			cache = read(text)
		case strings.HasPrefix(text, "anon "):
			rss = read(text)
		}
		if cache != 0 && rss != 0 {
			break // both found, stop scanning
		}
	}
	return
}

func extractCPU(s string) (user, system, total resources.MicroSecond) {
	read := func(line string, i *resources.MicroSecond) {
		num := line[strings.Index(line, " ")+1:]
		v, _ := strconv.ParseInt(num, 10, 64)
		*i = resources.MicroSecond(v)
	}
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		text := scanner.Text()
		switch {
		case strings.HasPrefix(text, "user_usec"):
			read(text, &user)
		case strings.HasPrefix(text, "system_usec"):
			read(text, &system)
		case strings.HasPrefix(text, "usage_usec"):
			read(text, &total)
		}
	}
	return
}

// blockPIDs blocks until there are no more live processes in the cgroup, and returns true
// if the timeout is exceeded or an error occurs.
func (e *exe) blockPIDs(timeout time.Duration) bool {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	abort := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			count := e.currentPIDs()
			switch count {
			case 0:
				// processes are no longer running
				return false
			case -1:
				// failed to read cgroups file, issue force kill
				return true
			default:
				// processes are still running, wait longer
			}
		case <-abort:
			// timeout exceeded, issue force kill
			return true
		}
	}
}

// currentPIDs returns the number of live processes in the cgroup.
func (e *exe) currentPIDs() int {
	s, err := e.readCG("pids.current")
	if err != nil {
		return -1
	}
	if s == "" {
		return 0
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return i
}
