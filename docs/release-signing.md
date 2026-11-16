# Release signing: two independent signatures

A release digest carries **two** signatures, and a consumer requires both
(`SECURITY.md`). Two signatures made from one place would be one signature,
because whoever can drive the first can drive the second.

- **GitLab, automatic, key-based.** `sign:release-digest` on every
  publishing pipeline -- a push to `preview` and a `v*` tag (#1472) --
  running on the dedicated `mikroview-signing` runner, signs the digest
  `publish:image` pushed, with a private key that exists only on that
  runner's host, and attests the image's SPDX SBOM to the same digest.
  The logic is `scripts/sign-release-digest.sh`, not the YAML.
- **GitHub, manual, keyless.** `.github/workflows/countersign.yml`,
  `workflow_dispatch` only. It verifies the key-based signature against the
  committed `cosign.pub` first and refuses on mismatch, absence, ambiguity or
  expiry. Requiring the owner's own GitHub login is the point: a bad release
  then needs two independent compromises on two hosts.

The public half is `cosign.pub` at the repository root. Its SHA-256 is
`48e61cd6732d65274276aeb12bace438c495c4a02911178cafd5cf59d67b8284`.

## What is signed: the digest GitLab built (#1472)

GitLab builds the image once and every later job handles that build's bytes
(build once, test the built bytes, promote by digest; orbit's shape). On a
push to `preview` or a `v*` tag:

1. `build:image` builds it (`VERSION` = `preview:<short sha>` or the tag) and
   saves `image.tar`.
2. `test:container`, `security:trivy-image` and, on a tag,
   `release:check-version` (tag equals `v<VERSION>`, binary reports the tag)
   all run on that tar. `security:trivy-image` writes `image.spdx.json`.
3. `publish:image` pushes it to `registry.tomlawson.io/ai/mikroview:sha-<commit>`,
   pulls the digest back to prove it is the tested image, and records it in
   `.mv-supply-chain/gitlab-tested-image.json` (90-day artifact).
4. `sign:release-digest` signs that recorded digest and attests the SBOM to
   it (`cosign attest --type spdxjson`). It refuses evidence for another
   repository or commit, a missing SBOM, a digest the registry does not
   confirm, and a multi-platform image.
5. `publish:channel` verifies the key-based signature against `cosign.pub`
   (`scripts/verify-release-signature.sh`) and only then names the digest
   `:preview` or `:vX.Y.Z`, registry-side, re-reading the digest afterwards.
6. On a tag, `record:upgrade-fixture` records the fixture from that digest.

`publish:channel` does not move `:preview` or `:vX.Y.Z` before the signature
verifies, but that is ordering, not protection. **The GitLab registry's
channel tags are internal:** any job token with push access, a merge-request
job's included, can push or move a tag there, so a tag in that registry proves
nothing on its own. Operators pull from GHCR, never from here. GitHub puts an
image on GHCR only after it has verified the key-based signature on the
GitLab digest it copies (below), and that check is what operators rely on.
Preview images are signed as well as releases (owner, #1472).

`publish:channel` holds the `publish-channel` resource group, so two
publishing pipelines never retag at once. GitLab does not run that group's
jobs in pipeline order, so a preview push also checks that its commit is still
`preview`'s head (`git ls-remote`) and fails, publishing nothing, when a newer
push has superseded it. That is how an older pipeline is kept from rolling
`:preview` back. Tags skip the check: a tag names one commit for good.
`latest` is not a GitLab registry tag: it stays GHCR's, moved by GitHub on the highest
release tag (below).

To check an image in the GitLab registry by hand (registry login needed):

```sh
cosign verify --key cosign.pub registry.tomlawson.io/ai/mikroview@<digest>
cosign verify-attestation --key cosign.pub --type spdxjson \
  registry.tomlawson.io/ai/mikroview@<digest>
```

## How the digest reaches GHCR (#1472 step 3)

GitHub builds nothing. `.github/workflows/publish-from-gitlab.yml` (a push to
`preview`) and `.github/workflows/release-on-tag.yml` (a `v*` tag) each:

1. wait for the commit's own GitLab push pipeline and fetch its evidence and
   SBOM (`scripts/ci/gitlab-await-tested-image.sh`), refusing unless
   `publish:image`, `sign:release-digest`, `publish:channel` and
   `security:trivy-image` all succeeded;
2. verify on the GitLab digest, against `cosign.pub`, exactly one key-based
   signature (`scripts/verify-release-signature.sh`) and exactly one SPDX
   attestation, whose predicate becomes the SBOM GitHub attests; stop before
   any GHCR write if either fails;
3. check the GitLab digest still resolves, then copy the image only (manifest
   and layers, by digest) to `ghcr.io/tomlawesome/mikroview` with
   `docker buildx imagetools create --prefer-index=false`, so the GHCR digest
   is the GitLab one: `preview-<short7>`, plus `preview` while the commit is
   still `preview`'s head; or `vX.Y.Z`, then `latest` when the tag is the
   highest `vX.Y.Z` in the repository. Each tag is read back and must resolve
   to the GitLab digest. The key-based signature and attestation are not
   copied: they stay in the GitLab registry (verify-at-source, as orbit
   does);
4. attest provenance, and the SBOM handed over from step 2 as a workflow
   artifact, on GHCR (`actions/attest`, in a job that never sees the GitLab
   token) and verify both with `gh attestation verify`.

`countersign.yml` then verifies the key-based signature and SBOM attestation
on the GitLab registry digest (read-only GitLab secrets), checks the digest is
on GHCR, adds the keyless signature there and verifies it. If a copy fails, GHCR keeps its
previous `preview`/`latest`; re-run the workflow once GitLab is green.
`docker.yml` is `workflow_dispatch` only and is deleted once two preview
publishes and one release have gone through this path.

Owner steps for the rest of #1472, none of which an assistant can do:

- create a project access token on ai/mikroview (Reporter, `read_api`,
  `read_registry`) and store it on GitHub as the secrets
  `GITLAB_READ_TOKEN_NAME` / `GITLAB_READ_TOKEN`;
- check the registry cleanup policy keeps `sha-*` for 7 days and `v*` and
  `preview` for good;
- delete the `GHCR_PUBLISH_TOKEN` CI/CD variable: nothing in this pipeline
  reads it any more;
- add `sha256-[0-9a-f]{64}.*` to the registry cleanup policy's keep-tags
  regex (and, if the registry uses the referrers API, confirm untagged
  manifests are not garbage-collected): the key-based signature and
  attestation now live only in the GitLab registry, so a release's evidence
  must outlive the 14-day `sha256-*` expiry. Rekor keeps the log entry regardless, and
  the publish run's `signature-verification.json` keeps the verification for
  90 days.

## The signature goes in the transparency log

`scripts/sign-release-digest.sh` does **not** pass `--tlog-upload=false`,
unlike orbit's key-based signing. `cosign verify --key` requires a log entry
by default, so a signature without one would fail the publish workflows' and
the countersignature's verify steps and the command SECURITY.md hands anyone
with read access to the GitLab registry.

The key-based signature stays in the GitLab registry (it is not copied to
GHCR), so the Rekor entry is its only public record: it shows the signature
was made, and it outlives the registry's retention.

## The job needs no Docker daemon

`docker buildx imagetools inspect` and `docker login` are registry
operations the CLI performs itself over HTTPS; cosign talks to the registry
directly too. The job reads `publish:image`'s evidence, so it never pulls or
loads the image. Verified against a real
GHCR tag with `DOCKER_HOST` pointed at a socket that does not exist
(2026-09-22), and it is the same shape orbit's `sign_evidence` job uses.

So **no Docker socket is mounted into the signing job, and none should be.**
The signing runner is the better-protected half of the pair; handing it
daemon access to earn a digest lookup would be a poor trade.

## Owner-only setup on the runner host

Assistants cannot do any of this: it needs `sudo` on `gitlab-runners`.

The host's Docker is **rootless**, run by `gitlab-runner` (uid 988). Root
inside the job container is uid 988 on the host, so root-owned key material
is unreadable there — it reads as `nobody`. The key must be owned by
`gitlab-runner`, not by root.

```sh
install -d -m 0700 -o gitlab-runner -g gitlab-runner /etc/mikroview-signing
install -m 0600 -o gitlab-runner -g gitlab-runner /path/to/cosign.key \
  /etc/mikroview-signing/cosign.key
( umask 077 && IFS= read -r -s -p 'key password: ' p && \
  printf '%s' "$p" > /etc/mikroview-signing/password ); echo
chown gitlab-runner:gitlab-runner /etc/mikroview-signing/password
```

`read -s` keeps the password off the command line and out of shell history.
Shred any copy left outside the directory — a key still sitting in a home
directory defeats the fence.

**Rootless Docker snapshots `/etc` when its daemon starts**, so a directory
created under `/etc` afterwards is invisible to it: the job sees an empty
mount and fails with "no password file" while the file is plainly there.
After creating the directory, restart that user's Docker once. It kills any
job then running on the host.

```sh
sudo -u gitlab-runner XDG_RUNTIME_DIR=/run/user/988 \
  DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/988/bus \
  systemctl --user restart docker
```

Later reboots need nothing: the directory exists before Docker starts. Both
traps were found on orbit's first `preview` run (pipeline 791, 2026-09-09)
and are recorded in `orbit/docs/releasing.md`.

The runner's `config.toml` entry carries the mount and the rootless socket —
without the latter the executor looks for `/var/run/docker.sock`, which does
not exist:

```toml
[runners.docker]
  host = "unix:///run/user/988/docker.sock"
  volumes = ["/etc/mikroview-signing:/etc/mikroview-signing:ro", "/cache"]
```

Confirmed present 2026-09-22, along with the runner being protected, locked
to this project, and refusing untagged jobs.

## Checking the setup without exposing anything

Run on the runner host. It prints file metadata and a hash of the *public*
key derived from the private one — never the key or the password:

```sh
sudo sh -c 'cd /etc/mikroview-signing || { echo "DIR MISSING"; exit 1; }; \
  for f in cosign.key password; do [ -s "$f" ] && \
    stat -c "%n  mode=%a owner=%U:%G size=%s" "$f" || echo "$f MISSING OR EMPTY"; done; \
  h=$(COSIGN_PASSWORD="$(cat password)" cosign public-key --key cosign.key 2>/dev/null \
    | sha256sum | cut -d" " -f1); \
  [ "$h" = "48e61cd6732d65274276aeb12bace438c495c4a02911178cafd5cf59d67b8284" ] \
    && echo "PAIRING OK" || echo "PAIRING FAIL - got: ${h:-nothing}"'
```

Both files should be owned by `gitlab-runner`, and the pairing should pass.
If cosign is not installed on that host the file checks still stand, and the
job does the real pairing check the first time it runs.

Until the key material is in place, `sign:release-digest` fails at its
preflight naming the path it could not find, before it touches the registry.
`publish:channel` then never runs, so no image is published unsigned.

## Exercising the signing job without cutting a release

A `v*` tag on this project **is** a release: the tag pipeline publishes it, and
the mirror job pushes it to GitHub. So there is no throwaway tag that
exercises signing without also cutting a release.

Instead `sign:release-digest` can be started by hand against an
already-published tag in this project's registry
(`registry.tomlawson.io/ai/mikroview:<tag>`, since #1472), and attests no SBOM.

**Since #1472 it signs only the digest this pipeline's own `publish:image`
tested and recorded.** A tag is never signed on its own say-so, because any job
token can move a tag in that registry. It refuses without that evidence
("publish:image must run and pass first") and refuses a tag that resolves to
any other digest ("not the recorded …"). A hand-started `SIGN_TAG` pipeline
runs no `publish:image`, so in practice the exercise now always refuses, and
no longer proves anything beyond the key mount and the refusal itself. To
re-sign an image, run a fresh publishing pipeline. `SIGN_TAG` is on the same `never` lists as
`CHR_EXERCISE` and `CHR_WATCH`, so triggering it does not re-run all of dev
(#951).

**`SIGN_TAG` is the only variable.** Naming a tag by hand is already
unambiguous intent, so an extra on/off switch beside it was one more name to
spell correctly and nothing else -- and it was misspelled on the first attempt
(pipeline 1483, 2026-09-22), which quietly ran the whole of dev and signed
nothing. If the job does not appear in a manual run, the variable name is the
first thing to check.

Owner decision, 2026-09-22: exercise it by **signing v0.6.0's digest for
real**, rather than dry-running, so the GHCR write and the signature push are
proved as well as the key mount and the digest lookup. Signing an
already-published release adds a valid second signature to it and changes
nothing about what that release contains.

Run it from the GitLab web UI, on a **protected** branch -- the signing runner
refuses unprotected refs, so `dev` works and a feature branch does not -- with:

| Variable | Value |
| --- | --- |
| `SIGN_TAG` | the published tag to sign, e.g. `v0.6.0` |

Only `sign:release-digest` should run. If other jobs appear, `SIGN_TAG` did not
reach the pipeline.

On a publishing pipeline there is no `SIGN_TAG` and the job signs the digest
`publish:image` recorded; without that evidence the script refuses rather than
sign something it guessed.

A retry is a no-op, not a second signature: before signing, the script asks
`cosign verify --key cosign.pub` about the digest and skips with `already
signed, skipping` if a signature verifies; before attesting, it does the same
with `verify-attestation --type spdxjson` (`already attested, skipping`). So if
`sign:release-digest` fails after its signature was pushed (say at the
attestation), re-run that job: it adds only what is missing. Exercising an
already-signed digest signs nothing. The job's `resource_group` runs one
signing job at a time, so two pipelines on the same digest cannot both pass
the check and both sign; a second key-based signature would make
`scripts/verify-release-signature.sh` refuse the digest as ambiguous.

## The exercise that was run, 2026-09-23

Before #1472, when the job signed GHCR's copy:

Pipeline 1484 on `dev`, `SIGN_TAG=v0.6.0`. One job, 37 seconds. It found the key
and password, logged in to GHCR, resolved exactly one platform manifest, and
pushed the signature:

```
sign-release-digest: signed ghcr.io/tomlawesome/mikroview@sha256:3a5e61e840cb23f2ec9e5c964b42ed6d1b6c0734936d9c0b5b7d7bd4fd5eb024
```

Verified independently afterwards with the public key, which reported the
signature valid and its claims present in the transparency log. Evidence on
#1309.

So v0.6.0's digest carries a key-based signature it did not ship with. That was
deliberate (owner, 2026-09-22): signing an already-published release adds a
valid second signature and changes nothing about what the release contains.

Afterwards, confirm the signature verifies with the public key the same way an
operator would:

```sh
cosign verify --key cosign.pub \
  ghcr.io/tomlawesome/mikroview@sha256:3a5e61e840cb23f2ec9e5c964b42ed6d1b6c0734936d9c0b5b7d7bd4fd5eb024
```
