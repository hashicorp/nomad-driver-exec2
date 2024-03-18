# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: BUSL-1.1

job "exit7" {
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

    task "script" {
      driver = "exec2"

      template {
        perms       = "555"
        destination = "local/exit7.sh"
        data        = <<EOH
#!/bin/sh
exit 7
EOH
      }

      config {
        command = "${NOMAD_TASK_DIR}/exit7.sh"
        unveil  = ["rx:${NOMAD_TASK_DIR}"]
      }

      resources {
        cpu    = 10
        memory = 10
      }
    }
  }
}
