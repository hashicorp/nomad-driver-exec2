# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "sleep" {
  type = "service"

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

    task "sleep" {
      driver = "exec2"

      config {
        command = "sleep"
        args    = ["infinity"]
      }

      resources {
        cpu    = 10
        memory = 10
      }
    }
  }
}
