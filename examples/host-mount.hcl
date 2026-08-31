# Copyright IBM Corp. 2024, 2026
# SPDX-License-Identifier: MPL-2.0

# Access a host path from inside an exec2 task.
# Landlock blocks the path even with POSIX permission — unveil is required.
# Host mounts are correctly propagated into the task namespace.
# /tmp/data must exist on the host before the job is submitted:
#   mkdir -p /tmp/data && echo "hello from host" > /tmp/data/test.txt

job "host-mount" {
  type = "batch"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    task "ls" {
      driver = "exec2"

      config {
        command = "ls"
        args    = ["-lh", "/tmp/data"]
        unveil  = ["r:/tmp/data"]
      }

      resources {
        cpu    = 100
        memory = 32
      }
    }
  }
}
