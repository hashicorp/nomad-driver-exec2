# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

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
        cpu    = 100
        memory = 32
      }
    }
  }
}
