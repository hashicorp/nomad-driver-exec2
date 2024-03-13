package main

import (
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-driver-exec2/plugin"
	"github.com/hashicorp/nomad/plugins"
)

func main() {
	plugins.Serve(factory)
}

func factory(logger hclog.Logger) interface{} {
	return plugin.New(logger)
}
