// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aexec

import (
	"context"
	"os/exec"

	"github.com/hashicorp/nomad/plugins/drivers"
)

func RunPlain(ctx context.Context, opts *drivers.ExecOptions) (*drivers.ExitResult, error) {
	command := opts.Command[0]
	args := opts.Command[1:]
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Stdin = opts.Stdin

	if err := cmd.Run(); err != nil {
		return &drivers.ExitResult{
			ExitCode: cmd.ProcessState.ExitCode(),
			Err:      err,
		}, err
	}

	return &drivers.ExitResult{
		ExitCode: 0,
	}, nil
}

func RunTTY(ctx context.Context) (*drivers.ExitResult, error) {
	return nil, nil
}
