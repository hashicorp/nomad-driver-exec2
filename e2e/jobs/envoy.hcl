# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "envoy" {
  type = "service"

  group "group" {

    network {
      mode = "host"
      port "lb" {}
    }

    task "envoy" {
      driver = "exec2"
      user   = "nobody"

      service {
        provider = "nomad"
        name     = "envoy"
        port     = "lb"
        tags     = ["envoy-test"]

        check {
          type     = "tcp"
          interval = "3s"
          timeout  = "1s"
        }
      }

      config {
        command = "/opt/bin/envoy"
        args    = ["-c", "${NOMAD_TASK_DIR}/local/envoy.yaml"]
        unveil  = ["rx:/opt/bin", "rwc:/dev/shm"]
      }

      resources {
        cpu    = 1000
        memory = 512
      }

      template {
        destination = "${NOMAD_TASK_DIR}/local/envoy.yaml"
        data        = <<EOF
static_resources:

  listeners:
  - name: listener_0
    address:
      socket_address:
        address: 0.0.0.0
        port_value: {{ env "NOMAD_PORT_lb" }}
    filter_chains:
    - filters:
      - name: envoy.filters.network.http_connection_manager
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          stat_prefix: ingress_http
          access_log:
          - name: envoy.access_loggers.stdout
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StdoutAccessLog
          http_filters:
          - name: envoy.filters.http.router
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
          route_config:
            name: local_route
            virtual_hosts:
            - name: local_service
              domains: ["*"]
              routes:
              - match:
                  prefix: "/"
                route:
                  host_rewrite_literal: www.envoyproxy.io
                  cluster: service_envoyproxy_io

  clusters:
  - name: service_envoyproxy_io
    type: LOGICAL_DNS
    # Comment out the following line to test on v6 networks
    dns_lookup_family: V4_ONLY
    load_assignment:
      cluster_name: service_envoyproxy_io
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: www.envoyproxy.io
                port_value: 443
    transport_socket:
      name: envoy.transport_sockets.tls
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext
        sni: www.envoyproxy.io
        EOF
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
