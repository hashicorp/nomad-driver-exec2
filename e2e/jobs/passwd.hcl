# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: BUSL-1.1

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
        cpu    = 10
        memory = 10
      }
    }
  }
}
