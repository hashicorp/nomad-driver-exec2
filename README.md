# Nomad exec2 Driver

The `exec2` task driver plugin is a modern alternative to Nomad's original
`exec` and `raw_exec` drivers. It offers a security model optimized for running
'ordinary' processes with very low startup times and minimal overhead in terms
of CPU and memory utilization. `exec2` leverages kernel features such as the
[Landlock LSM](https://docs.kernel.org/security/landlock.html), cgroups v2, and
ordinary file system permissions.

Ready-to-use job examples are in the [`examples/`](examples/) directory.

### Requirements

- Linux 5.15+
- Cgroups v2 enabled
- Landlock LSM enabled
- Commands `unshare` and `nsenter`
- Nomad client running as root

Recent mainstream Linux distributions such as Ubuntu 22.04 and Fedora 36 meet
the requirements and are well supported. RHEL does not currently enable Landlock
and therefore cannot be supported.

### When to Use exec2

`exec2` is the right choice when:

- You want to run an **ordinary Linux process** (a binary, a script, a JVM
  program, a Python service) directly on the Nomad client without a container
  runtime.
- You need **fast startup times** and minimal overhead — `exec2` starts tasks
  in microseconds with no image pull or container shim in the path.
- You want stronger isolation than `raw_exec` provides. `exec2` uses Landlock
  for filesystem isolation and cgroups v2 for resource limits, giving you a
  meaningful security boundary without the full weight of a container.
- Your binary or runtime is already installed on the Nomad node (or delivered
  via a Nomad artifact / template block).

`exec2` is **not** the right choice when:

- Your workload requires OCI image distribution or a container-level network
  namespace — use the `docker` or `podman` drivers instead.
- The node does not have Landlock enabled (e.g. RHEL without a custom kernel).

### Simple Example

Here is a simple example running `env`. It makes use of a dynamic workload
user and does not require any extra filepaths to `unveil`. More examples are
at the bottom of this README and in the [`examples/`](examples/) directory.

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
    allow_caps      = ["net_bind_service", "chown", "kill", ...]
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

  - `allow_caps` - (default: 13 Nomad-default capabilities) - an operator
  allowlist of Linux capability names that tasks on this node are permitted to
  request. Capability names are case-insensitive and accept all common formats
  (`net_bind_service`, `NET_BIND_SERVICE`, `CAP_NET_BIND_SERVICE`). Set to `[]`
  to disallow all capability grants on this node.

  ```hcl
  # allow tasks to request net_bind_service only
  allow_caps = ["net_bind_service"]

  # disallow all capability grants
  allow_caps = []
  ```

  The default set is:
  `audit_write`, `chown`, `dac_override`, `fowner`, `fsetid`, `kill`, `mknod`,
  `net_bind_service`, `setfcap`, `setgid`, `setpcap`, `setuid`, `sys_chroot`.

#### Task Configuration

##### config

Task configuration for an `exec2` task includes setting a `command`, `args` for
the command, and additional `unveil` paths if `unveil_by_task` is enabled in
plugin configuration.


```hcl
config {
  command       = "/usr/bin/cat"
  args          = ["/etc/os-release"]
  unveil        = ["r:/etc/os-release"]
  oom_score_adj = 500
  cap_add       = ["net_bind_service"]
  cap_drop      = []
  work_dir      = "/path/to/workdir"
}
```

  - `command` - (required) - The command to run. Note that this filepath is
  not automatically made accessible to the task. For example, an executable
  under `/opt/bin` would not be accessible unless granted access through `unveil`
  in task config or `unveil_paths` in plugin config.

  - `args` - (optional) - A list of arguments to provide to `command`.

  - `unveil` - (optional) - A list of additional filesystem paths to provide
  access to the task (requires `unveil_by_task` in plugin config).

  - `oom_score_adj` - (optional) - The likelihood of the task being OOM killed,
  must be a positive integer. Defaults to `0`.

  - `cap_add` - (optional) - A list of Linux capability names to add as ambient
  capabilities for the task process. The effective capability set starts empty;
  `cap_add` adds to it. Each name must be present in the operator's `allow_caps`
  plugin config or the task is rejected at start time. Capability names are
  case-insensitive and accept all common formats (`net_bind_service`,
  `NET_BIND_SERVICE`, `CAP_NET_BIND_SERVICE`). Defaults to `[]`.

  - `cap_drop` - (optional) - A list of Linux capability names to remove from
  the set assembled by `cap_add`. Names are normalized the same way as `cap_add`.
  Useful for dropping a specific capability when `cap_add` was set broadly.
  Defaults to `[]`.

  ```hcl
  # grant CAP_NET_BIND_SERVICE so the task can bind port 80 without root
  cap_add = ["net_bind_service"]

  # grant two caps then selectively revoke one
  cap_add  = ["net_bind_service", "chown"]
  cap_drop = ["chown"]
  ```

  - `work_dir` - (optional) - Override the default working directory for the
  task. Accepts an absolute path or a path relative to the task directory
  parent. Defaults to `$NOMAD_TASK_DIR` (the task's local directory).

  ```hcl
  # set CWD to the shared alloc directory
  work_dir = "${NOMAD_ALLOC_DIR}"
  ```

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
driver.exec2.caps.allowlist     = audit_write,chown,dac_override,fowner,fsetid,kill,mknod,net_bind_service,setfcap,setgid,setpcap,setuid,sys_chroot
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

### Common Examples

More complete, production-oriented examples with explanatory comments are in the
[`examples/`](examples/) directory.

#### python http

This example demonstrates using Python's built-in HTTP server to serve the contents
of the task's own task directory. Notice the use of `unveil` to give the task access
to `/etc/mime.types` with read-only permissions, which is required for Python's HTTP
server to operate correctly.

```hcl
job "http" {
  group "web" {
    network {
      mode = "host"
      port "http" { static = 8181 }
    }

    task "python" {
      driver = "exec2"

      config {
        command = "python3"
        args    = ["-m", "http.server", "${NOMAD_PORT_http}", "--directory", "${NOMAD_TASK_DIR}"]
        unveil  = ["r:/etc/mime.types"]
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
```

#### java programs

This example demonstrates the use of `java` to run a `Test.class` file
located in the task's allocation directory. The JVM requires certain files
on the host to work properly; on this system the task must unveil the
`/etc/java-17-openjdk` path. On GitHub CI runners this might be located
under `/etc/alternatives`, for example, and the `javabin` variable would
need to be `/usr/lib/jvm/temurin-21-jdk-amd64/bin`.

```hcl
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

  group "group" {
    task "main" {
      driver = "exec2"

      config {
        command = "${var.javabin}/java"
        args    = ["-cp", "${NOMAD_ALLOC_DIR}", "Test"]
        unveil  = ["r:${var.etcjava}"]
      }
    }
  }
}
```
