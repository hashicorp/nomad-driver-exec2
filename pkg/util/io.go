// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package util

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	a, err := openpipe(stdout)
	if err != nil {
		return nil, nil, err
	}
	b, err := openpipe(stderr)
	if err != nil {
		return nil, nil, err
	}
	return a, b, nil
}

func openpipe(path string) (io.WriteCloser, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("error opening fifo parent directory %q: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	// also uses O_NOFOLLOW under the hood
	f, err := root.OpenFile(base, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("error opening writer at %s: %w", path, err)
	}
	return f, nil
}
