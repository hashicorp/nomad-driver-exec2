// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/hashicorp/nomad/plugins/drivers"
	dproto "github.com/hashicorp/nomad/plugins/drivers/proto"
)

// Ensure Plugin implements ExecTaskStreamingRawDriver at compile time.
// Nomad's gRPC server prefers this interface over ExecTaskStreaming when a
// driver implements it.
var _ drivers.ExecTaskStreamingRawDriver = (*Plugin)(nil)

// ExecTaskStreamingRaw implements drivers.ExecTaskStreamingRawDriver.
// It enters the running task's Linux namespaces via nsenter and runs the
// requested command. When tty is true it opens a real PTY so interactive
// shells work correctly; otherwise it uses plain pipes.
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

	pid, netns := h.ExecInfo()
	args := append(nsenterArgs(pid, netns), command...)
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
	// Close slave in the parent — the child inherited its own copy.
	pts.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// stdin + resize: gRPC stream → PTY master
	go func() {
		for {
			msg, err := stream.Recv()
			if isExecStreamClosed(err) {
				return
			}
			if err != nil {
				errCh <- err
				return
			}
			if msg.Stdin != nil {
				if len(msg.Stdin.Data) > 0 {
					if _, err := ptm.Write(msg.Stdin.Data); err != nil {
						errCh <- err
						return
					}
				}
				if msg.Stdin.Close {
					return
				}
			} else if msg.TtySize != nil {
				// Forward terminal resize events so the process sees the
				// correct window size (required for editors, less, etc.)
				_ = pty.Setsize(ptm, &pty.Winsize{
					Rows: uint16(msg.TtySize.Height),
					Cols: uint16(msg.TtySize.Width),
				})
			}
		}
	}()

	// stdout: PTY master → gRPC stream
	// In TTY mode stderr is merged into stdout by the PTY itself.
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := ptm.Read(buf)
			if n > 0 {
				_ = stream.Send(&drivers.ExecTaskStreamingResponseMsg{
					Stdout: &dproto.ExecTaskStreamingIOOperation{Data: buf[:n]},
				})
			}
			if isExecStreamClosed(err) {
				_ = stream.Send(&drivers.ExecTaskStreamingResponseMsg{
					Stdout: &dproto.ExecTaskStreamingIOOperation{Close: true},
				})
				return
			}
			if err != nil {
				errCh <- err
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	// Closing PTM unblocks the stdout reader goroutine.
	ptm.Close()
	wg.Wait()

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

	// stdin: gRPC stream → cmd
	go func() {
		for {
			msg, err := stream.Recv()
			if isExecStreamClosed(err) {
				stdinW.Close()
				return
			}
			if err != nil {
				errCh <- err
				return
			}
			if msg.Stdin != nil {
				if len(msg.Stdin.Data) > 0 {
					if _, err := stdinW.Write(msg.Stdin.Data); err != nil {
						errCh <- err
						return
					}
				}
				if msg.Stdin.Close {
					stdinW.Close()
				}
			}
		}
	}()

	// stdout: cmd → gRPC stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardExecOutput(stdoutR,
			func(b []byte) *drivers.ExecTaskStreamingResponseMsg {
				return &drivers.ExecTaskStreamingResponseMsg{
					Stdout: &dproto.ExecTaskStreamingIOOperation{Data: b},
				}
			},
			func() *drivers.ExecTaskStreamingResponseMsg {
				return &drivers.ExecTaskStreamingResponseMsg{
					Stdout: &dproto.ExecTaskStreamingIOOperation{Close: true},
				}
			},
			send, errCh,
		)
	}()

	// stderr: cmd → gRPC stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardExecOutput(stderrR,
			func(b []byte) *drivers.ExecTaskStreamingResponseMsg {
				return &drivers.ExecTaskStreamingResponseMsg{
					Stderr: &dproto.ExecTaskStreamingIOOperation{Data: b},
				}
			},
			func() *drivers.ExecTaskStreamingResponseMsg {
				return &drivers.ExecTaskStreamingResponseMsg{
					Stderr: &dproto.ExecTaskStreamingIOOperation{Close: true},
				}
			},
			send, errCh,
		)
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

// isExecStreamClosed returns true for errors that indicate the stream or pipe
// has been cleanly closed and no further I/O should be attempted.
//
//   - io.EOF / io.ErrClosedPipe — pipe write-end closed (no-TTY path)
//   - syscall.EIO — PTY slave closed after the process exited (normal TTY exit)
//   - syscall.EBADF — PTY master was explicitly closed (ptm.Close at line 142)
//     before the stdout goroutine finished its current read loop iteration;
//     semantically identical to EIO for our purposes
func isExecStreamClosed(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF || err == io.ErrClosedPipe {
		return true
	}
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EIO || errno == syscall.EBADF
	}
	return false
}
