# Release signing: two independent signatures

A release digest carries **two** signatures, and a consumer requires both
(`SECURITY.md`). Two signatures made from one place would be one signature,
because whoever can drive the first can drive the second.

- **GitLab, automatic, key-based.** `sign:release-digest` on the `v*` tag
  pipeline, running on the dedicated `mikroview-signing` runner, signs the
  released digest with a private key that exists only on that runner's host.
  The logic is `scripts/sign-release-digest.sh`, not the YAML.
- **GitHub, manual, keyless.** `.github/workflows/countersign.yml`,
  `workflow_dispatch` only. It verifies the key-based signature against the
  committed `cosign.pub` first and refuses on mismatch, absence, ambiguity or
  expiry. Requiring the owner's own GitHub login is the point: a bad release
  then needs two independent compromises on two hosts.

The public half is `cosign.pub` at the repository root. Its SHA-256 is
`48e61cd6732d65274276aeb12bace438c495c4a02911178cafd5cf59d67b8284`.

## The signature goes in the transparency log

`scripts/sign-release-digest.sh` does **not** pass `--tlog-upload=false`,
unlike orbit's key-based signing. `cosign verify --key` requires a log entry
by default, so a signature without one would fail both the countersignature's
verify step and the plain command SECURITY.md hands operators.

The difference is the registry: orbit signs into its own private registry,
where nothing is publicly verifiable anyway, while this image is public on
GHCR and the whole point of the pair is that an operator can check both
signatures without being told to pass `--insecure-ignore-tlog`.

## The job needs no Docker daemon

`docker manifest inspect`, `docker buildx imagetools inspect` and
`docker login` are all registry operations the CLI performs itself over
HTTPS; cosign talks to the registry directly too. Verified against a real
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
preflight naming the path it could not find — before the 45-minute wait for
GHCR, so a misplaced key fails fast rather than an hour in.
