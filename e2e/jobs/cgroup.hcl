# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: BUSL-1.1

job "cgroup" {
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
        command = "/usr/bin/cat"
        args    = ["/proc/self/cgroup"]
        unveil  = ["r:/proc/self/cgroup"]
      }

      resources {
        cpu    = 100
        memory = 16
      }
    }
  }
}
