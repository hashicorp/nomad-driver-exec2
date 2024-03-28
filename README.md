# Nomad exec2 Driver

The `exec2` Nomad task driver offers improvements over the original `exec`
task driver historically built into the Nomad binary. `exec2` makes use of
modern Linux kernel features such as the Landlock LSM and cgroups v2 to provide
filesystem and system resource isolation.

### Requirements

- Ubuntu 22.04 / CentOS 9 or later
- Cgroups v2 enabled
- Landlock LSM enabled
- Nomad client running as root

### Example Jobs

Here is a simple example running `env`. It makes use of a dynamic workload
user and does not require any extra filepaths to `unveil`.

```hcl
job "env" {
  type = "batch"
  group "group" {
    task "task" {
      driver = "exec2"
      config {
        command = "env"
      }
    }
  }
}
```

### Concepts

#### Filesystem Isolation

The `exec2` driver makes use of [`go-landlock`](https://github.com/shoenig/go-landlock)
for providing filesystem isolation, making the host filesystem unreachable except
where explicitly allowed.

By default a task is enabled to access its task directory and its shared alloc
directory. The paths to these directories are accessible by reading the
environment variables `$NOMAD_TASK_DIR` and `$NOMAD_ALLOC_DIR` respectively.

A file access mode must also be specified when granting additional filesystem
access to a path. This is done by prefixing the path with `'r'`, `'w'`, `'x'`,
and/or `'c'` indicating read, write, executable, and create permissions, 
respectively. e.g.,

  - `r:/srv/www` - read-only access to `/srv/www`
  - `rwc:/tmp` - read, write, and create files in `/tmp`
  - `rx:/opt/bin/application` - read and execute a specific application
  - `wc:/var/log` - write and create files in `/var/log`

#### Resource Isolation

Similar to `exec` and other container runtimes, `exec2` makes use of cgroups
for limiting the amount of CPU and RAM a task may consume.

### Configuration

#### Plugin Configuration

The default plugin configuration is shown below. System default paths are
enabled, but nothing else. These default paths enable basic functionality like
reading system TLS certificates, executing programs in `/bin`, `/usr/bin`, and
accessing shared object files. The exact set of default paths is system
dependent, and can be disabled or customized in plugin config.

Additional allowable paths can be specified at the plugin level, which applies
to all tasks making use of the `exec2` driver, or at the task level, which will
apply specifically to each task.

```hcl
plugin "nomad-driver-exec2" {
  config {
    unveil_defaults = true
    unveil_paths    = []
    unveil_by_task  = false
  }
}
```

  - `unveil_defaults` - (default: `true`) - enable or disable default system
  paths useful for running basic commands

  - `unveil_paths` - (default: `[]`) - a list of filesystem paths with permissions
  to grant to all tasks

  ```hcl
  unveil_paths = ["rx:/opt/bin", "r:/srv/certs"]
  ```

  - `unveil_by_task` - (default: `false`) - enable or disable job submitters to
  specify additional filesystem path access within task config

#### Task Configuration

Task configuration for an `exec2` task includes setting a `command`, `args` for
the command, and additional `unveil` paths if `unveil_by_task` is enabled in
plugin configuration.

```hcl
config {
  command = "/usr/bin/cat"
  args    = ["/etc/os-release"]
  unveil  = ["r:/etc/os-release"]
}
```

  - `command` - (required) - command to run

  - `args` - (optional) - list of arguments to provide to `command`

  - `unveil` - (optional) - list of additional filesystem paths to provide
  access to the task (requires `unveil_by_task` in plugin config)

### Attributes

When installed, the `exec2` plugin provides the following node attributes which
can be used as constraints when authoring jobs.

```text
driver.exec2.unveil.defaults    = true
driver.exec2.unveil.tasks       = true
```

### Install

The `exec2` driver is an external Nomad task-driver plugin. It can be compiled
from source or downloaded from [HashiCorp](https://releases.hashicorp.com/nomad-driver-exec2/).

See the [Nomad Task Driver](https://developer.hashicorp.com/nomad/docs/drivers)
documentation for getting started with using external task driver plugins.

### Hacking

For local development, the included [Makefile] includes a `hack` target which
builds the plugin and launches Nomad in `-dev` mode with the plugin directory
set to include the development build of the plugin.

There are two test suites - one for unit tests and one for a small e2e test
suite which runs the plugin through a real Nomad client. To run the e2e suite,
set `GOFLAGS=-tags=e2e` and run `go test` in the `e2e` directory. A Nomad client
must already be running.

```shell-session
make hack
```
