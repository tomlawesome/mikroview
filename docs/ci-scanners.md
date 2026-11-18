# CI scanners

Six scanners run in the GitLab pipeline for #1462 (supply-chain and static
checks) and #1463 (npm provenance). All are CI-only: none is installed in, or
shipped with, the MikroView image or binary.

| Job | Tool | Licence | Looks at |
| --- | --- | --- | --- |
| `lint:shellcheck` | shellcheck | GPL-3.0 | every `*.sh`, warning and up |
| `lint:golangci` | golangci-lint v2 | GPL-3.0 | Go, `.golangci.yml` (standard set plus `dupl`; `gosec` stays with `security:gosec`) |
| `lint:ruff` | ruff | MIT | Python, `ruff.toml` (defaults plus the `S` security rules; narrow per-file ignores, each with a reason) |
| `lint:knip` | knip | ISC | `frontend/`, `frontend/knip.jsonc` |
| `lint:jscpd` | jscpd | MIT | `frontend/src`, `.jscpd.json` |
| `security:zizmor` | zizmor | MIT | `.github/workflows/`, offline, no token |

Every scanner blocks: `lint:shellcheck`, `lint:golangci`, `lint:ruff`,
`security:zizmor`, `lint:knip` and `lint:jscpd` fail the pipeline on a finding
(#1462, #1463). Each job writes its full report to a file kept as an artifact
for 30 days.

`lint:knip` has no baseline: its findings are zero, so any unused export, file
or dependency fails. `lint:jscpd` runs jscpd with exit code 0 and then
`scripts/jscpd-baseline.mjs` compares the clone list with
`frontend/.jscpd-baseline.txt` (each clone keyed by its two files, lines and
tokens): a clone not listed fails, and so does a listed entry that matches no
clone. The baseline only shrinks; fix a clone and delete its line. The job also
runs `npm audit signatures` after `npm ci` and fails if a registry signature or
provenance attestation is invalid. The baseline script's test
(`scripts/jscpd-baseline.test.mjs`) runs in `lint:script-tests`.

Pins: the shellcheck, golangci-lint and ruff images by digest (in
`supply-chain/pins-policy.json`); zizmor by a `sha256sum -c` of the release
tarball; knip and jscpd by exact version in `frontend/package.json` and the
lockfile. Renovate reads the images and npm pins, and the zizmor version
through a regex manager; a zizmor bump must update `ZIZMOR_SHA256` by hand.
