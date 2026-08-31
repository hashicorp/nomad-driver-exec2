# Copyright IBM Corp. 2024, 2026
# SPDX-License-Identifier: MPL-2.0

job "cap_add" {
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
        args    = ["/proc/self/status"]
        unveil  = ["r:/proc/self/status"]
        cap_add = ["net_bind_service"]
      }

      resources {
        cpu    = 100
        memory = 32
      }
    }
  }
}
