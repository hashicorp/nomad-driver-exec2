# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

schema = 1
artifacts {
  zip = [
    "nomad-driver-exec2_${version}_linux_amd64.zip",
    "nomad-driver-exec2_${version}_linux_arm64.zip",
  ]
  rpm = [
    "nomad-driver-exec2-${version_linux}-1.aarch64.rpm",
    "nomad-driver-exec2-${version_linux}-1.x86_64.rpm",
  ]
  deb = [
    "nomad-driver-exec2_${version_linux}-1_amd64.deb",
    "nomad-driver-exec2_${version_linux}-1_arm64.deb",
  ]
}
