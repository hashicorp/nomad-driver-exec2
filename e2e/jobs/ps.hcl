# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: BUSL-1.1

job "ps" {
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

    task "ps" {
      driver = "exec2"
      user   = "root"

      config {
        command = "ps"
        args    = ["aux"]
        unveil  = ["r:/proc"]
      }

      resources {
        cpu    = 10
        memory = 16
      }
    }
  }
}
