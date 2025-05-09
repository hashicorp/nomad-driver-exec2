// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"flag"
	"fmt"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-driver-exec2/plugin"
	"github.com/hashicorp/nomad-driver-exec2/version"
	"github.com/hashicorp/nomad/plugins"
)

func main() {
	printVersion := false
	flag.BoolVar(&printVersion, "version", printVersion, "print version and exit")
	flag.Parse()
	if printVersion {
		fmt.Println(version.Full())
		return
	}

	plugins.Serve(factory)
}

func factory(logger hclog.Logger) any {
	return plugin.New(logger)
}
