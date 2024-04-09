# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "passwd" {
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

    task "cat" {
      driver = "exec2"

      config {
        command = "cat"
        args    = ["/etc/passwd"]
      }

      resources {
        cpu    = 100
        memory = 32
      }
    }
  }
}
