# Nomad exec2 Driver

The `exec2` task driver plugin is a modern alternative to Nomad's original
`exec` and `raw_exec` drivers. It offers a security model optimized for running
'ordinary' processes with very low startup times and minimal overhead in terms
of CPU and memory utilization. `exec2` leverages kernel features such as the
[Landlock LSM](https://docs.kernel.org/security/landlock.html), cgroups v2, and
ordinary file system permissions.

### Requirements

- Linux 5.15+
- Cgroups v2 enabled
- Landlock LSM enabled
- Commands `unshare` and `nsenter`
- Nomad client running as root

Recent mainstream Linux distributions such as Ubuntu 22.04 and RHEL 9 meet the
requirements and are well supported.

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

##### landlock

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

This style of permission control is modeled after the `unveil` system call
introduced by the OpenBSD project. In configuration parameters we refer to the
"unveil"-ing of filesystem paths as `exec2` is leveraging landlock to emulate
the semantics of `unveil`.

##### dynamic workload users

While landlock prevents tasks from accessing the host filesystem, Nomad 1.8
introduces `dynamic workload users` which enable tasks to be run as a PID/GID
that is not assigned to any user. This provides protection from non-root users
getting access inside the task and allocation directories created for the task.

To make use of a dynamic workload user, simply leave the `user` field blank
in the task definition of an `exec2` task.

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

The default set of default paths are listed below. These paths are enabled only
if they are found to exist at the time of the task launching.

##### bin files

- `/bin` (read, execute)
- `/usr/bin` (read, execute)
- `/usr/local/bin` (read, execute)

##### shared objects

- `/dev/null` (read, write)
- `/lib` (read, execute)
- `/lib64` (read, execute)
- `/usr/lib` (read, execute)
- `/usr/libexec` (read, execute)
- `/usr/local/lib` (read, execute)
- `/usr/local/lib64` (read, execute)
- `/etc/ld.so.conf` (read)
- `/etc/ld.so.cache` (read)
- `/etc/ld.so.conf.d` (read)

##### io, common

- `/tmp` (read, write, create)
- `/dev/full` (read, write)
- `/dev/zero` (read)
- `/dev/fd` (read)
- `/dev/stdin` (read, write)
- `/dev/stdout` (read, write)
- `/dev/urandom` (read)
- `/dev/log` (write)
- `/usr/share/locale` (read)
- `/proc/self/cmdline` (read)
- `/usr/share/zoneinfo` (read)
- `/usr/share/common-licenses` (read)
- `/proc/sys/kernel/ngroups_max` (read)
- `/proc/sys/kernel/cap_last_cap` (read)
- `/proc/sys/vm/overcommit_memory` (read)

##### dns

- `/etc/hosts` (read)
- `/hostname` (read)
- `/etc/services` (read)
- `/etc/protocols` (read)
- `/etc/resolv.conf` (read)

##### certificates

- `/etc/ssl/certs` (read)
- `/etc/pki/tls/certs` (read)
- `/sys/etc/security/cacerts` (read)
- `/etc/ssl/ca-bundle.pem` (read)
- `/etc/pki/tls/cacert.pem` (read)
- `/etc/pki/ca-trust-extracted/pem/tls-ca-bundle.pem` (read)
- `/etc/ssl/cert.pem` (read)

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

##### config

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

  - `command` - (required) - The command to run. Note that this filepath is
  not automatically made accessible to the task. For example, an executable
  under `/opt/bin` would not be accessible unless granted access through `unveil`
  in task config or `unveil_paths` in plugin config.

  - `args` - (optional) - A list of arguments to provide to `command`.

  - `unveil` - (optional) - A list of additional filesystem paths to provide
  access to the task (requires `unveil_by_task` in plugin config).

##### cpu

Tasks can be limited in CPU resources by setting the `cpu` or `cores` values
in the task `resources` block.

  - `cpu` - (default: `100`) - limits the CPU bandwidth allowable for the task
  to make use of in MHz, may not be used with `cores`

  - `cores` - (optional) - specifies the number of CPU cores to reserve
  exclusively for the task, may not be used with `cpu`

##### memory

Tasks can be limited in memory resources by setting `memory` and optionally the
`memory_max` values in the task `resources` block.

  - `memory` - (default: `300`) - specifies the memory required in MB

  - `memory_max` - (optional) - specifies the maximum memory the task may use
  if the client has excess memory capacity and [memory oversubscription](https://developer.hashicorp.com/nomad/docs/job-specification/resources#memory-oversubscription)
  is enabled for the cluster/node pool.

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
