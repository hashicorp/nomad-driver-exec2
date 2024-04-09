# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "http" {
  type = "service"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    network {
      mode = "host"
      port "http" { static = 8181 }
    }

    reschedule {
      attempts  = 0
      unlimited = false
    }

    restart {
      attempts = 0
      mode     = "fail"
    }

    task "http" {
      driver = "exec2"

      config {
        command = "python3"
        args    = ["-m", "http.server", "${NOMAD_PORT_http}", "--directory", "${NOMAD_TASK_DIR}"]
      }

      resources {
        cpu    = 500
        memory = 128
      }

      template {
        destination = "local/index.html"
        data        = <<EOH
<!doctype html>
<html>
  <title>example</title>
  <body><p>Hello, user!</p></body>
</html>
EOH
      }
    }
  }
}
