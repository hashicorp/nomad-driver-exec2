# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "oom_score_adj" {
  type = "service"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    task "oom_score_adj" {
      driver = "exec2"

      config {
        command       = "sleep"
        args          = ["infinity"]
        oom_score_adj = 500
      }
    }
  }
}
