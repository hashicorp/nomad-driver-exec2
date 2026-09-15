# Copyright IBM Corp. 2024, 2026
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

ui {
  show_cli_hints = false
}

client {
  enabled = true
  options = {
    "fingerprint.denylist" = "env_aws,env_gce,env_azure,env_digitalocean"
  }

  # host volume used by the e2e volume-mount test
  host_volume "exec2-e2e-volume" {
    path = "/tmp/exec2-e2e-volume"
  }
}

plugin "nomad-driver-exec2" {
  config {
    unveil_by_task = true
    unveil_paths   = ["r:/etc/mime.types"]
  }
}
