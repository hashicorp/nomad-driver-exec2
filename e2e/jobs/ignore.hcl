# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: BUSL-1.1

job "ignore" {
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

    task "ignore" {
      driver       = "exec2"
      kill_timeout = "5s"
      kill_signal  = "SIGINT"

      config {
        command = "python3"
        args    = ["${NOMAD_TASK_DIR}/ignore.py"]
      }

      resources {
        cpu    = 100
        memory = 16
      }

      template {
        destination = "local/ignore.py"
        data        = <<EOH
import signal
import os
import time

if __name__ == '__main__':
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    signal.pause()
EOH
      }
    }
  }
}
