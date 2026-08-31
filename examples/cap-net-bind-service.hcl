# Copyright IBM Corp. 2024, 2026
# SPDX-License-Identifier: MPL-2.0

# Bind a privileged port without running as root using an ambient capability.
# net_bind_service is in the default allow_caps list — no extra plugin config needed.
# Requires unveil_by_task = true in plugin config for the unveil field to take effect.

job "cap-net-bind-service" {
  type = "service"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    network {
      mode = "host"
      port "http" { static = 80 }
    }

    task "http" {
      driver = "exec2"

      config {
        command = "python3"
        args    = ["-m", "http.server", "${NOMAD_PORT_http}", "--directory", "${NOMAD_TASK_DIR}"]
        cap_add = ["net_bind_service"]
        unveil  = ["r:/etc/mime.types"]
      }

      template {
        destination = "local/index.html"
        data        = <<EOH
<!doctype html>
<html>
  <head><title>exec2 cap_add example</title></head>
  <body><p>Bound port 80 via cap_add = ["net_bind_service"]</p></body>
</html>
EOH
      }

      resources {
        cpu    = 200
        memory = 64
      }
    }
  }
}
