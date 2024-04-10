// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package util

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// NullCloser returns an implementation of io.WriteCloser that will always
// succeed on the call to Close().
func NullCloser(w io.Writer) io.WriteCloser {
	return &writeCloser{w}
}

type writeCloser struct {
	io.Writer
}

func (*writeCloser) Close() error {
	return nil
}

// OpenPipes opens the given paths for standard IO as named pipes.
//
// The returned values must be closed by the caller.
func OpenPipes(stdout, stderr string) (io.WriteCloser, io.WriteCloser, error) {
	a, err := os.OpenFile(stdout, unix.O_WRONLY, os.ModeNamedPipe)
	if err != nil {
		return nil, nil, err
	}
	b, err := os.OpenFile(stderr, unix.O_WRONLY, os.ModeNamedPipe)
	if err != nil {
		return nil, nil, err
	}
	return a, b, nil
}
