# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "mktemp" {
  type = "batch"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    reschedule {
      attempts  = 0
      unlimited = false
    }

    restart {
      attempts = 0
      mode     = "fail"
    }

    task "mktemp" {
      driver = "exec2"

      config {
        command = "mktemp"
      }

      resources {
        cpu    = 100
        memory = 32
      }
    }
  }
}
