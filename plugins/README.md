# Official task plugin sources

The JavaScript files under `tasks/` are unmodified releases by QuantumNous from
[new-api-plugins](https://github.com/QuantumNous/new-api-plugins), licensed under
Apache-2.0. The full license is in [tasks/LICENSE](tasks/LICENSE).

`SOURCES.json` records the exact upstream commit, paths, versions and published
SHA-256 pins. Sources are copied from Git blobs, preserving the published bytes
(a Windows checkout may use different line endings). Do not edit a published
script in place; adopt a new reviewed version and retain the previous identity.

Bundling does not install, activate or certify a plugin's models, SDK features,
provider access or paid settlement. Installation and deployment validation are
separate operations. Upstream usage guides and changelogs are available at the
recorded repository and commit beside each original source.

The runtime uses MoeJS v0.1.0 (Apache-2.0) and moego v0.1.0-alpha.3 (MIT).
Their license and third-party notice texts are retained in `runtime-licenses/`
and embedded with the sources. The SDK host is independently implemented from
the published API v1 contract; the upstream gateway host is not vendored.
