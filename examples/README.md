# exec2 Examples

Production-oriented job templates for the `exec2` Nomad task driver — each file is a
self-contained, copy-paste starting point. For full driver docs see the top-level
[README.md](../README.md).

## Examples

| File | What it shows |
|------|---------------|
| [`script.hcl`](script.hcl) | Shell script via `template` block — `perms = "555"` + `rx:` unveil both required |
| [`cap-net-bind-service.hcl`](cap-net-bind-service.hcl) | Bind port 80 without root using `cap_add = ["net_bind_service"]` |
| [`host-mount.hcl`](host-mount.hcl) | Access a host path via `unveil` — Landlock blocks it even with POSIX perms |

## e2e test fixtures (`e2e/jobs/`)

These are internal fixtures run by `make e2e`. They have `restart.attempts = 0` and
`reschedule.attempts = 0` for deterministic test assertions — not for production use.

| File | What it tests |
|------|---------------|
| [`env.hcl`](../e2e/jobs/env.hcl) | `NOMAD_SHORT_ALLOC_ID` and dynamic `USER` env vars |
| [`sleep.hcl`](../e2e/jobs/sleep.hcl) | Long-running service, graceful stop |
| [`http.hcl`](../e2e/jobs/http.hcl) | Python HTTP server with `unveil` |
| [`java.hcl`](../e2e/jobs/java.hcl) | Java compile + run via `prestart` task |
| [`cap_add.hcl`](../e2e/jobs/cap_add.hcl) | `cap_add` sets ambient capabilities (`CapAmb` non-zero) |
| [`work_dir.hcl`](../e2e/jobs/work_dir.hcl) | `work_dir` overrides task CWD to `NOMAD_ALLOC_DIR` |
| [`passwd.hcl`](../e2e/jobs/passwd.hcl) | Landlock blocks `/etc/passwd` read (job must die) |
| [`secret.hcl`](../e2e/jobs/secret.hcl) | Workload identity token via secrets API |
| [`oom_score_adj.hcl`](../e2e/jobs/oom_score_adj.hcl) | `oom_score_adj` written to `/proc/<pid>/oom_score_adj` |
| [`ps.hcl`](../e2e/jobs/ps.hcl) | PID namespace isolation — only shim + task visible |
| [`cgroup.hcl`](../e2e/jobs/cgroup.hcl) | Correct cgroup path in `/proc/self/cgroup` |
| [`resources.hcl`](../e2e/jobs/resources.hcl) | cgroup memory/cpu limits match Nomad resource spec |
