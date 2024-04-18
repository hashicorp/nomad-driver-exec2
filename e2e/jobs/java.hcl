# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

variable "javabin" {
  type    = string
  default = "/usr/bin"
}

variable "etcjava" {
  type    = string
  default = "/etc/java-17-openjdk"
}

job "java" {
  type = "batch"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {
    task "compile" {
      driver = "exec2"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      template {
        destination = "local/Test.java"
        data        = <<EOF
public class Test {
  public static void main(String[] args) throws Exception {
    System.out.println("hello, java!");
  }
}
        EOF
      }

      config {
        command = "${var.javabin}/javac"
        args    = ["-d", "${NOMAD_ALLOC_DIR}", "local/Test.java"]
        unveil  = ["r:${var.etcjava}"]
      }

      resources {
        cpu    = 1000
        memory = 512
      }
    }

    task "main" {
      driver = "exec2"

      config {
        command = "${var.javabin}/java"
        args    = ["-cp", "${NOMAD_ALLOC_DIR}", "Test"]
        unveil  = ["r:${var.etcjava}"]
      }

      resources {
        cpu    = 1000
        memory = 512
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
