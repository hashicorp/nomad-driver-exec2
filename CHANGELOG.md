## UNRELEASED

BUG FIXES:

* Fixed silent discard of `unshare`/`nsenter` stderr; errors now appear in `nomad alloc logs`. [[GH-61](https://github.com/hashicorp/nomad-driver-exec2/pull/61)]

IMPROVEMENTS:

* Added a `-version` flag to print the plugin version and git revision instead of the default go-plugin message, making it easier to verify which binary is deployed. [[GH-61](https://github.com/hashicorp/nomad-driver-exec2/pull/61)]
* Log lines emitted from the shim now include `alloc_id` and `task_name` for easier correlation in multi-tenant environments. [[GH-61](https://github.com/hashicorp/nomad-driver-exec2/pull/61)]

## 0.1.0 (October 15, 2024)

GA release of exec2! v0.1.0

## 0.1.0-beta.2 (July 18, 2024)

IMPROVEMENTS:

* Implemented support for setting `oom_score_adj` on a per-task basis. [GH-40](https://github.com/hashicorp/nomad-driver-exec2/pull/40)
* Updated the Linux packaging to install into the default Nomad plugin directory. [GH-39](https://github.com/hashicorp/nomad-driver-exec2/pull/39)

BUG FIXES:

* Fixed a bug where the temp directory created for tasks was not available. [GH-38](https://github.com/hashicorp/nomad-driver-exec2/pull/38)

## 0.1.0-beta.1 (June 5, 2024)

First beta release of initial v0.1.0 version.

## 0.1.0-alpha.2 (May 24, 2024)

Second alpha release of initial v0.1.0 version.
