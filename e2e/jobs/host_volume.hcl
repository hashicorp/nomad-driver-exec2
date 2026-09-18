# Copyright IBM Corp. 2024, 2026
# SPDX-License-Identifier: MPL-2.0

# Mounts a read-only host volume into the task and reads a seeded file from it.

job "host_volume" {
  type = "batch"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    volume "data" {
      type      = "host"
      source    = "exec2-e2e-volume"
      read_only = true
    }

    task "task" {
      driver = "exec2"

      volume_mount {
        volume      = "data"
        destination = "${NOMAD_ALLOC_DIR}/mounted"
        read_only   = true
      }

      config {
        command = "cat"
        args    = ["${NOMAD_ALLOC_DIR}/mounted/hello.txt"]
      }

      resources {
        cpu    = 500
        memory = 128
      }
    }

    restart {
      attempts = 0
      mode     = "fail"
    }

    reschedule {
      attempts  = 0
      unlimited = false
    }
  }
}
