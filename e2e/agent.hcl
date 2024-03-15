
plugin "nomad-driver-exec2" {
  config {
    unveil_paths = ["r:/etc/mime.types"]
  }
}
