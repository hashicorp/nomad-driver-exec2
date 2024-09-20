// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-driver-exec2/plugin"
	"github.com/hashicorp/nomad/plugins"
)

// hello

func main() {
	plugins.Serve(factory)
}

func factory(logger hclog.Logger) any {
	return plugin.New(logger)
}
