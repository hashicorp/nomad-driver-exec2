# Nomad exec2 Driver

The `exec2` Nomad task driver offers improvements over the original `exec` task
driver historically built into the Nomad binary. `exec2` makes use of modern
Linux kernel features such as the Landlock LSM and cgroups v2 to provide filesystem
and system resource isolation.
