// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"encoding/json"
	"fmt"
	"os"
)

// configVersion is the current version of the ShimConfig format.
// Increment this if the format changes in a backward-incompatible way.
const configVersion = 1

// ShimConfig holds all parameters the exec2-shim subprocess needs at startup.
type ShimConfig struct {
	// Version identifies the config format. Always set to configVersion (1).
	Version int `json:"version"`

	// UnveilDefaults controls whether the shim should enable the default
	// Landlock unveil paths (task dir, alloc dir, etc.).
	UnveilDefaults bool `json:"unveil_defaults"`

	// OutPipe is the filesystem path to the named pipe for task stdout.
	OutPipe string `json:"out_pipe"`

	// ErrPipe is the filesystem path to the named pipe for task stderr.
	ErrPipe string `json:"err_pipe"`

	// UID is the numeric user ID the task process should run as.
	UID int `json:"uid"`

	// GID is the numeric group ID the task process should run as.
	GID int `json:"gid"`

	// Capabilities is the list of Linux capability names to raise as ambient
	// capabilities for the task process. Empty means no extra capabilities.
	Capabilities []string `json:"capabilities"`

	// UnveilPaths is the list of additional Landlock filesystem paths to expose,
	// in "mode:path" format (e.g. "r:/some/path", "rwxc:/alloc/data").
	UnveilPaths []string `json:"unveil_paths"`

	// Command is the executable to run as the task process.
	Command string `json:"command"`

	// Arguments are the command-line arguments passed to Command.
	Arguments []string `json:"arguments"`
}

// WriteShimConfig marshals cfg as JSON and atomically writes it to dir/name.
// All file operations are performed through dir (an os.Root) so that symlinks
// cannot redirect the write or rename outside the task directory — the same
// pattern used by fixpipe and openpipe elsewhere in this package.
// The data is first written to name+".tmp" then renamed into place,
// so the shim never reads a partially-written file.
func WriteShimConfig(dir *os.Root, name string, cfg *ShimConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("exec2: marshal shim config: %w", err)
	}

	tmp := name + ".tmp"

	// root.OpenFile refuses to follow symlinks that escape the root,
	// preventing a task from redirecting our write to an arbitrary path.
	f, err := dir.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("exec2: create shim config tmp: %w", err)
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		_ = dir.Remove(tmp)
		return fmt.Errorf("exec2: write shim config tmp: %w", werr)
	}
	if cerr != nil {
		_ = dir.Remove(tmp)
		return fmt.Errorf("exec2: close shim config tmp: %w", cerr)
	}

	// Rename within the root — both names are plain basenames so no escape
	// is possible, and rename(2) replaces the destination atomically.
	if err = dir.Rename(tmp, name); err != nil {
		_ = dir.Remove(tmp)
		return fmt.Errorf("exec2: rename shim config: %w", err)
	}

	return nil
}

// ReadShimConfig reads and unmarshals a ShimConfig from path.
// The returned error wraps fs.ErrNotExist when the file is absent, so callers
// can distinguish "file not found" from a corrupt or malformed config.
func ReadShimConfig(path string) (*ShimConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("exec2: read shim config: %w", err)
	}

	var cfg ShimConfig
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("exec2: unmarshal shim config: %w", err)
	}

	return &cfg, nil
}
