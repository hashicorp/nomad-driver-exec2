# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "resources" {
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

    task "memory.max" {
      user   = "nobody"
      driver = "exec2"

      config {
        command = "cat"
        args    = ["/sys/fs/cgroup/nomad.slice/share.slice/${NOMAD_ALLOC_ID}.${NOMAD_TASK_NAME}.scope/memory.max"]
        unveil  = ["r:/sys/fs/cgroup/nomad.slice"]
      }
      resources {
        cpu    = 100
        memory = 200
      }
    }

    task "memory.max.oversub" {
      user   = "nobody"
      driver = "exec2"
      config {
        command = "cat"
        args    = ["/sys/fs/cgroup/nomad.slice/share.slice/${NOMAD_ALLOC_ID}.${NOMAD_TASK_NAME}.scope/memory.max"]
        unveil  = ["r:/sys/fs/cgroup/nomad.slice"]
      }
      resources {
        cpu        = 100
        memory     = 150
        memory_max = 250
      }
    }

    task "memory.low.oversub" {
      user   = "nobody"
      driver = "exec2"
      config {
        command = "cat"
        args    = ["/sys/fs/cgroup/nomad.slice/share.slice/${NOMAD_ALLOC_ID}.${NOMAD_TASK_NAME}.scope/memory.low"]
        unveil  = ["r:/sys/fs/cgroup/nomad.slice"]
      }
      resources {
        cpu        = 100
        memory     = 150
        memory_max = 250
      }
    }

    task "cpu.max" {
      user   = "nobody"
      driver = "exec2"
      config {
        command = "cat"
        args    = ["/sys/fs/cgroup/nomad.slice/share.slice/${NOMAD_ALLOC_ID}.${NOMAD_TASK_NAME}.scope/cpu.max"]
        unveil  = ["r:/sys/fs/cgroup/nomad.slice"]
      }
      resources {
        cpu = 1000
      }
    }

    task "cpu.max.cores" {
      user   = "nobody"
      driver = "exec2"
      config {
        command = "cat"
        args    = ["/sys/fs/cgroup/nomad.slice/reserve.slice/${NOMAD_ALLOC_ID}.${NOMAD_TASK_NAME}.scope/cpu.max"]
        unveil  = ["r:/sys/fs/cgroup/nomad.slice"]
      }
      resources {
        cores = 1
      }
    }
  }
}
