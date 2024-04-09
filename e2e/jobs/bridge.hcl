# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "bridge" {
  type = "service"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    network {
      mode = "bridge"
      port "http" { to = 8181 }
    }

    reschedule {
      attempts  = 0
      unlimited = false
    }

    restart {
      attempts = 0
      mode     = "fail"
    }

    service {
      provider = "nomad"
      name     = "homepage"
      port     = "http"
      check {
        name     = "up"
        type     = "http"
        path     = "/index.html"
        interval = "6s"
        timeout  = "1s"
      }
    }

    task "python" {
      driver = "exec2"

      config {
        command = "python3"
        args    = ["-m", "http.server", "8181", "--directory", "${NOMAD_TASK_DIR}"]
      }

      template {
        destination = "local/index.html"
        data        = <<EOH
<!doctype html>
<html>
  <title>bridge mode</title>
  <body><p>Hello, bridge!</p></body>
</html>
EOH
      }

      resources {
        cpu    = 350
        memory = 128
      }
    }
  }
}

