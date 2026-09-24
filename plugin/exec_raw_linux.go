// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/hashicorp/nomad/plugins/drivers"
	dproto "github.com/hashicorp/nomad/plugins/drivers/proto"
)

// ExecTaskStreamingRaw services the driver ExecTaskStreaming RPC via the
// drivers.ExecTaskStreamingRawDriver interface. It enters the task's
// namespaces with nsenter and runs the command, handling both requests: when
// tty is true it allocates a PTY for an interactive shell, otherwise it wires
// stdin/stdout/stderr over pipes.
func (p *Plugin) ExecTaskStreamingRaw(
	ctx context.Context,
	taskID string,
	command []string,
	tty bool,
	stream drivers.ExecTaskStream,
) error {
	h, exists := p.tasks.Get(taskID)
	if !exists {
		return drivers.ErrTaskNotFound
	}

	pid := h.ExecInfo()
	args := append(nsenterArgs(pid), command...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	if tty {
		return execRawTTY(cmd, stream)
	}
	return execRawNoTTY(cmd, stream)
}

// execRawTTY runs cmd with a real PTY so interactive shells (nomad alloc exec -t)
// work correctly. The PTY master bridges the gRPC stream and the process.
func execRawTTY(cmd *exec.Cmd, stream drivers.ExecTaskStream) error {
	// Open a PTY pair: ptm is the master (our side), pts is the slave (process side).
	ptm, pts, err := pty.Open()
	if err != nil {
		return fmt.Errorf("exec streaming: open pty: %w", err)
	}
	defer ptm.Close()

	// Wire all three stdio streams to the slave end of the PTY.
	// The process sees a real terminal and behaves interactively.
	cmd.Stdin = pts
	cmd.Stdout = pts
	cmd.Stderr = pts
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true, // new session — process becomes session leader
		Setctty: true, // pts becomes the controlling terminal of the session
	}

	if err := cmd.Start(); err != nil {
		pts.Close()
		return fmt.Errorf("exec streaming tty: start: %w", err)
	}
	// Close slave in the parent
	pts.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// stdin + resize → PTY master. ptmMu guards ptm access, ptmClosed 
	// makes the stdin goroutine stop touching ptm once it is closed. 
	var (
		ptmMu     sync.Mutex
		ptmClosed bool
	)
	ptmWrite := func(p []byte) error {
		ptmMu.Lock()
		defer ptmMu.Unlock()
		if ptmClosed {
			return os.ErrClosed
		}
		_, err := ptm.Write(p)
		return err
	}
	ptmResize := func(height, width int) error {
		ptmMu.Lock()
		defer ptmMu.Unlock()
		if ptmClosed {
			return os.ErrClosed
		}
		return pty.Setsize(ptm, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
	}
	// Not in wg: blocks on stream.Recv() until the RPC returns.
	go handleExecStdin(stream, ptmWrite, ptmResize, nil, errCh)

	// stdout: PTY master → stream (stderr is merged into stdout by the PTY).
	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardExecOutput(ptm, stdoutDataMsg, stdoutCloseMsg, stream.Send, errCh)
	}()

	waitErr := cmd.Wait()
	// Unblock the stdout goroutine's ptm.Read.
	_ = ptm.SetDeadline(time.Now())
	wg.Wait()
	// Mark ptm closed under the lock, this waits for any in-flight Write/Setsize
	// and blocks the stdin goroutine from touching ptm before the deferred Close.
	ptmMu.Lock()
	ptmClosed = true
	ptmMu.Unlock()

	// Send the final exit result back to Nomad.
	_ = stream.Send(buildExecExitResult(cmd.ProcessState, waitErr))

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// execRawNoTTY runs cmd without a PTY, using plain pipes for stdin/stdout/stderr.
func execRawNoTTY(cmd *exec.Cmd, stream drivers.ExecTaskStream) error {
	var mu sync.Mutex
	send := func(msg *drivers.ExecTaskStreamingResponseMsg) error {
		mu.Lock()
		defer mu.Unlock()
		return stream.Send(msg)
	}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	defer stdoutW.Close()
	defer stderrW.Close()

	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("exec streaming: start: %w", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	// stdin → cmd. stdinW closes only on explicit Stdin.Close; stream-close
	// teardown is handled by the caller below.
	stdinWrite := func(p []byte) error {
		_, err := stdinW.Write(p)
		return err
	}
	go handleExecStdin(stream, stdinWrite, nil, func() { _ = stdinW.Close() }, errCh)

	// stdout: cmd → gRPC stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardExecOutput(stdoutR, stdoutDataMsg, stdoutCloseMsg, send, errCh)
	}()

	// stderr: cmd → gRPC stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardExecOutput(stderrR, stderrDataMsg, stderrCloseMsg, send, errCh)
	}()

	waitErr := cmd.Wait()
	stdinR.Close()
	stdoutW.Close()
	stderrW.Close()
	wg.Wait()

	_ = stream.Send(buildExecExitResult(cmd.ProcessState, waitErr))

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// handleExecStdin forwards stdin data and TTY resize events from the stream to
// the process. resize is nil for non-TTY execs. closeStdin is invoked only on
// an explicit client Stdin.Close, and is nil when the destination must not be
// closed here (e.g. a PTY master shared with stdout)
func handleExecStdin(
	stream drivers.ExecTaskStream,
	write func([]byte) error,
	resize func(height, width int) error,
	closeStdin func(),
	errCh chan<- error,
) {
	for {
		msg, err := stream.Recv()
		if isExecStreamClosed(err) {
			return
		}
		if err != nil {
			errCh <- err
			return
		}

		switch {
		case msg.Stdin != nil:
			if len(msg.Stdin.Data) > 0 {
				if werr := write(msg.Stdin.Data); werr != nil {
					if isExecStreamClosed(werr) {
						return
					}
					errCh <- werr
					return
				}
			}
			if msg.Stdin.Close {
				if closeStdin != nil {
					closeStdin()
				}
				return
			}
		case msg.TtySize != nil && resize != nil:
			if rerr := resize(int(msg.TtySize.Height), int(msg.TtySize.Width)); rerr != nil {
				if isExecStreamClosed(rerr) {
					return
				}
				errCh <- rerr
				return
			}
		}
	}
}

func stdoutDataMsg(b []byte) *drivers.ExecTaskStreamingResponseMsg {
	return &drivers.ExecTaskStreamingResponseMsg{
		Stdout: &dproto.ExecTaskStreamingIOOperation{Data: b},
	}
}

func stdoutCloseMsg() *drivers.ExecTaskStreamingResponseMsg {
	return &drivers.ExecTaskStreamingResponseMsg{
		Stdout: &dproto.ExecTaskStreamingIOOperation{Close: true},
	}
}

func stderrDataMsg(b []byte) *drivers.ExecTaskStreamingResponseMsg {
	return &drivers.ExecTaskStreamingResponseMsg{
		Stderr: &dproto.ExecTaskStreamingIOOperation{Data: b},
	}
}

func stderrCloseMsg() *drivers.ExecTaskStreamingResponseMsg {
	return &drivers.ExecTaskStreamingResponseMsg{
		Stderr: &dproto.ExecTaskStreamingIOOperation{Close: true},
	}
}

// forwardExecOutput copies from r to the gRPC stream using the provided
// message builders for data and close events.
func forwardExecOutput(
	r io.Reader,
	dataMsg func([]byte) *drivers.ExecTaskStreamingResponseMsg,
	closeMsg func() *drivers.ExecTaskStreamingResponseMsg,
	send func(*drivers.ExecTaskStreamingResponseMsg) error,
	errCh chan<- error,
) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if serr := send(dataMsg(buf[:n])); serr != nil {
				errCh <- serr
				return
			}
		}
		if isExecStreamClosed(err) {
			_ = send(closeMsg())
			return
		}
		if err != nil {
			errCh <- err
			return
		}
	}
}

// buildExecExitResult constructs the final exit message sent to Nomad after
// the exec'd process terminates.
func buildExecExitResult(ps *os.ProcessState, err error) *drivers.ExecTaskStreamingResponseMsg {
	code := -2
	if ps != nil {
		if status, ok := ps.Sys().(syscall.WaitStatus); ok {
			code = status.ExitStatus()
			if status.Signaled() {
				// Preserve signal exit codes (128 + signal number),
				// matching the convention used by shells and Docker.
				code = 128 + int(status.Signal())
			}
		}
	} else if ee, ok := err.(*exec.ExitError); ok && ee.ProcessState != nil {
		if status, ok := ee.ProcessState.Sys().(syscall.WaitStatus); ok {
			code = status.ExitStatus()
		}
	}
	return &drivers.ExecTaskStreamingResponseMsg{
		Exited: true,
		Result: &dproto.ExitResult{ExitCode: int32(code)},
	}
}

// isExecStreamClosed reports whether err means the stream or pipe was cleanly
// closed and no further I/O should be attempted.
func isExecStreamClosed(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF || err == io.ErrClosedPipe {
		return true
	}
	if errors.Is(err, os.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	// EIO is returned by the kernel when the PTY slave has been closed; it may
	// arrive as a raw syscall.Errno or wrapped inside an *os.PathError.
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			return errno == syscall.EIO || errno == syscall.EBADF
		}
	}
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EIO || errno == syscall.EBADF
	}
	return false
}
