# Copyright IBM Corp. 2024, 2026
# SPDX-License-Identifier: MPL-2.0

# Run a shell script placed by a template block.
# command must be an absolute path; task CWD is ${NOMAD_TASK_DIR}.
# unveil = ["rx:..."] is required in addition to perms = "555" — Landlock
# blocks execution unless the execute bit is explicitly granted.

job "script" {
  type = "batch"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    task "script" {
      driver = "exec2"

      template {
        perms       = "555"
        destination = "local/run.sh"
        data        = <<EOH
#!/bin/sh
set -eu
echo "hostname: $(hostname)"
echo "task dir: ${NOMAD_TASK_DIR}"
EOH
      }

      config {
        command = "${NOMAD_TASK_DIR}/run.sh"
        unveil  = ["rx:${NOMAD_TASK_DIR}"]
      }

      resources {
        cpu    = 100
        memory = 32
      }
    }
  }
}
