// Copyright IBM Corp. 2024, 2026
// SPDX-License-Identifier: MPL-2.0

package shim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// configVersion is the current version of the ShimConfig format.
// Increment this if the format changes in a backward-incompatible way.
const configVersion = 1

// shimConfigName is the filename, relative to the task directory, of the
// serialized ShimConfig that the exec2-shim reads at startup.
const shimConfigName = ".shim_config.json"

// ShimConfig holds all parameters the exec2-shim subprocess needs at startup.
type ShimConfig struct {
	// Version identifies the config format. Always set to configVersion (1).
	Version int `json:"version"`

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

	// UnveilPaths is the complete list of Landlock entries to expose.
	// Each entry is either a "mode:path" pair (e.g. "r:/some/path", "rwxc:/alloc/data")
	// or built-ins (e.g. UnveilShared, UnveilCerts) that the shim expands
	// to a go-landlock path set.
	UnveilPaths []string `json:"unveil_paths"`

	// Command is the executable to run as the task process.
	Command string `json:"command"`

	// Arguments are the command-line arguments passed to Command.
	Arguments []string `json:"arguments"`
}

// write marshals the config as JSON and atomically writes it into taskDir as
// shimConfigName, returning the full path to the written file.
// The data is first written to a temporary file and then renamed into place, so the
// shim never reads a partially-written config.
func (cfg *ShimConfig) write(taskDir string) (string, error) {
	if taskDir == "" {
		return "", fmt.Errorf("exec2: task directory is not set")
	}

	root, err := os.OpenRoot(taskDir)
	if err != nil {
		return "", fmt.Errorf("exec2: open task dir for shim config: %w", err)
	}
	defer func() { _ = root.Close() }()

	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("exec2: marshal shim config: %w", err)
	}

	tmp := shimConfigName + ".tmp"

	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("exec2: create shim config tmp: %w", err)
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		_ = root.Remove(tmp)
		return "", fmt.Errorf("exec2: write shim config tmp: %w", werr)
	}
	if cerr != nil {
		_ = root.Remove(tmp)
		return "", fmt.Errorf("exec2: close shim config tmp: %w", cerr)
	}

	if err = root.Rename(tmp, shimConfigName); err != nil {
		_ = root.Remove(tmp)
		return "", fmt.Errorf("exec2: rename shim config: %w", err)
	}

	return filepath.Join(taskDir, shimConfigName), nil
}

// readShimConfig reads and unmarshals a ShimConfig from path.
func readShimConfig(path string) (*ShimConfig, error) {
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
