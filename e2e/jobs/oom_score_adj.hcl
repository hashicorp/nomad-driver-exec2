# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "oom" {
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

    task "oom" {
      driver = "exec2"

      config {
        command       = "sleep"
        args          = ["infinity"]
        oom_score_adj = 500
      }

      resources {
        cpu    = 100
        memory = 32
      }
    }
  }
}
