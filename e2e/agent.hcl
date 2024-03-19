# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

# intended to be used in conjunction with -dev mode; namely by invoking the
# 'make hack' Makefile target

server {
  enabled          = true
  bootstrap_expect = 1
  default_scheduler_config {
    memory_oversubscription_enabled = true
  }
}

client {
  enabled = true
  options = {
    "fingerprint.denylist" = "env_aws,env_gce,env_azure,env_digitalocean"
  }
}

plugin "nomad-driver-exec2" {
  config {
    unveil_by_task = true
    unveil_paths   = ["r:/etc/mime.types"]
  }
}
