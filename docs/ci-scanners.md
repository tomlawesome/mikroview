# CI scanners

Six scanners run in the GitLab pipeline for #1462 (supply-chain and static
checks) and #1463 (npm provenance). All are CI-only: none is installed in, or
shipped with, the MikroView image or binary.

| Job | Tool | Licence | Looks at |
| --- | --- | --- | --- |
| `lint:shellcheck` | shellcheck | GPL-3.0 | every `*.sh`, warning and up |
| `lint:golangci` | golangci-lint v2 | GPL-3.0 | Go, `.golangci.yml` (standard set plus `dupl`; `gosec` stays with `security:gosec`) |
| `lint:ruff` | ruff | MIT | Python, `ruff.toml` (defaults plus the `S` security rules) |
| `lint:knip` | knip | ISC | `frontend/`, `frontend/knip.json` |
| `lint:jscpd` | jscpd | MIT | `frontend/src`, `.jscpd.json` |
| `security:zizmor` | zizmor | MIT | `.github/workflows/`, offline, no token |

Each job is report-only until #1462/#1463 switch it to blocking: it is
`allow_failure`, and writes its full report to a file kept as an artifact for
30 days. The exception is the signature check in `lint:jscpd`: it runs
`npm audit signatures` after `npm ci` and fails the job if a registry signature
or provenance attestation is invalid. jscpd's own duplication result exits 3,
the only code the job tolerates.

Pins: the shellcheck, golangci-lint and ruff images by digest (in
`supply-chain/pins-policy.json`); zizmor by a `sha256sum -c` of the release
tarball; knip and jscpd by exact version in `frontend/package.json` and the
lockfile. Renovate reads the images and npm pins, and the zizmor version
through a regex manager; a zizmor bump must update `ZIZMOR_SHA256` by hand.
