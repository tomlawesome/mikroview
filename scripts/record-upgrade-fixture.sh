#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# scripts/record-upgrade-fixture.sh [--postgres] <version> -- boots the
# *released* image ghcr.io/tomlawesome/mikroview:<version> on empty
# storage, drives the scripted session in
# scripts/upgrade-fixture-session.py (an admin, a viewer, an API token, a
# named entity, a watchlist entry, a flag, a coverage declaration, a host
# mark -- each only where that version's API has it), stops it, and keeps
# what it wrote.
#
# Two modes, one session, so the two recordings of a release are
# comparable:
#
#   (default)    file backend. Packs the data directory as
#                .upgrade-fixtures/upgrade-fixture-<version>.tar.gz and
#                writes testdata/upgrade/<version>/manifest.json (what was
#                created -- no hashes, tokens, keys or passwords, see
#                docs/upgrades.md). Uploads the tarball and the manifest.
#
#   --postgres   the same session against a throwaway postgres:18-alpine
#                container, dumped with pg_dump (plain SQL, gzip) to
#                .upgrade-fixtures/upgrade-fixture-<version>-postgres.sql.gz
#                and uploaded. Postgres is proven per *schema* version
#                rather than per release (docs/decisions/
#                upgrade-framework.md), so only the first release carrying
#                each migration needs one: v0.1.0 (schema 1, store_blob),
#                v0.2.0 (schema 2, match_log) and v0.3.0 (schema 3,
#                match_log.provisional). No manifest is written here --
#                the file mode's is the one manifest for the release, and
#                the Postgres test reads it.
#
# This is the ONE time an old image runs (docs/decisions/
# upgrade-framework.md, "Proof is recordings, not booted images"). The
# routine gate never boots one; it only opens what this script recorded,
# via scripts/fetch-upgrade-fixtures.sh, internal/persist/upgrade_test.go
# and internal/persist/upgrade_postgres_test.go.
#
# Usage:
#   scripts/record-upgrade-fixture.sh v0.3.0
#   scripts/record-upgrade-fixture.sh --postgres v0.3.0
#
# Requires docker. Uploading takes whichever credential the environment
# offers:
#
#   CI_JOB_TOKEN set  -- curl PUT with a JOB-TOKEN header against
#                        $CI_API_V4_URL/projects/$CI_PROJECT_ID/... This is
#                        the path .gitlab-ci.yml's record:upgrade-fixture
#                        job takes on a release tag.
#   otherwise         -- `glab` already configured against the GitLab host
#                        that hosts this project (see
#                        ~/.config/agents/skills/github-credentials -- this
#                        script does not set GLAB_CONFIG_DIR/GITLAB_HOST
#                        itself, it uses whatever the caller's shell has).
#
# Set MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD=1 to record and pack locally
# without uploading (used by the "tiny file" upload test and by anyone who
# wants to inspect a recording before it goes anywhere).
set -euo pipefail

MODE="file"
VERSION=""
while [ $# -gt 0 ]; do
  case "$1" in
    --postgres) MODE="postgres" ;;
    --) ;;
    -*) echo "record-upgrade-fixture: unknown flag $1" >&2; exit 2 ;;
    *) VERSION="$1" ;;
  esac
  shift
done
: "${VERSION:?usage: scripts/record-upgrade-fixture.sh [--postgres] <version>, e.g. v0.3.0}"
VERSION_BARE="${VERSION#v}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="ghcr.io/tomlawesome/mikroview:${VERSION}"
# Pinned like every other image this repository runs (#711). Not in
# supply-chain/pins-policy.json: that file records the pins CI itself
# declares in .gitlab-ci.yml and the Dockerfile, and these two are
# already there for the jobs that name them.
HELPER_IMAGE="alpine:3.24"
PG_IMAGE="postgres:18-alpine"
SUFFIX=""
[ "$MODE" = "postgres" ] && SUFFIX="-pg"
CONTAINER_NAME="upgrade-fixture-${VERSION}${SUFFIX}"
VOLUME_NAME="upgrade-fixture-data-${VERSION}${SUFFIX}"
PG_CONTAINER="upgrade-fixture-postgres-${VERSION}"
NETWORK_NAME="upgrade-fixture-net-${VERSION}"
GITLAB_PROJECT_ID="${MIKROVIEW_GITLAB_PROJECT_ID:-53}"

FIXTURE_DIR="$ROOT/.upgrade-fixtures"
MANIFEST_DIR="$ROOT/testdata/upgrade/${VERSION}"
TARBALL="$FIXTURE_DIR/upgrade-fixture-${VERSION}.tar.gz"
DUMP="$FIXTURE_DIR/upgrade-fixture-${VERSION}-postgres.sql.gz"

log() { echo "record-upgrade-fixture[$VERSION/$MODE]: $*" >&2; }

WORKDIR=""
cleanup() {
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
  docker volume rm -f "$VOLUME_NAME" >/dev/null 2>&1 || true
  docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
  [ -z "$WORKDIR" ] || rm -rf "$WORKDIR"
}
trap cleanup EXIT

# upload <local-file> <name-in-the-package> -- puts one file at
# upgrade-fixtures/<version>/<name> in this project's generic package
# registry, with whichever credential the environment has.
upload() {
  local src="$1" name="$2" path url
  path="projects/${GITLAB_PROJECT_ID}/packages/generic/upgrade-fixtures/${VERSION}/${name}"
  if [ -n "${CI_JOB_TOKEN:-}" ]; then
    : "${CI_API_V4_URL:?record-upgrade-fixture: CI_JOB_TOKEN is set but CI_API_V4_URL is not}"
    : "${CI_PROJECT_ID:?record-upgrade-fixture: CI_JOB_TOKEN is set but CI_PROJECT_ID is not}"
    url="${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/generic/upgrade-fixtures/${VERSION}/${name}"
    # --upload-file is a PUT, which is what the generic registry wants.
    # The token goes in a header, never in the URL, so it stays out of
    # the job log and out of any redirect.
    curl -fsS --upload-file "$src" -H "JOB-TOKEN: ${CI_JOB_TOKEN}" "$url" >/dev/null
  else
    glab api -X PUT "$path" --input "$src" >/dev/null
  fi
  log "uploaded $name"
}

# version_ge A B -- true if version A is >= B, both without the leading v.
version_ge() {
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

# Feature generations, from reading each tag's internal/api/server.go
# (#1239): POST /api/definitions (the watchlist) landed at v0.3.0; GET/PUT
# /api/coverage/declarations and /api/hosts/{key}/mark landed at v0.5.0.
# The same v0.5.0 boundary also turned on #853 (state-store encryption
# under history.keyFile) -- without a key mounted, flags/entities/
# watchlist/coverage/hosts are memory-only on those two versions and
# nothing would be left to pack.
HAS_WATCHLIST=false
HAS_COVERAGE=false
NEEDS_HISTORY_KEY=false
version_ge "$VERSION_BARE" "0.3.0" && HAS_WATCHLIST=true
version_ge "$VERSION_BARE" "0.5.0" && { HAS_COVERAGE=true; NEEDS_HISTORY_KEY=true; }
GENERATION="A"
$HAS_WATCHLIST && GENERATION="B"
$HAS_COVERAGE && GENERATION="C"

# v0.1.0 is syslog UDP/TCP on :1514, plain -- issue #188 (TLS on :6514)
# landed at v0.2.0. Every later tag speaks RouterOS's remote-protocol=tls
# on :6514, which is what every other generation here assumes.
SYSLOG_PORT=6514
SYSLOG_MODE="tls"
if [ "$VERSION_BARE" = "0.1.0" ]; then
  SYSLOG_PORT=1514
  SYSLOG_MODE="plain"
fi

log "generation $GENERATION (watchlist=$HAS_WATCHLIST coverage/hosts=$HAS_COVERAGE history-key=$NEEDS_HISTORY_KEY syslog=$SYSLOG_MODE)"

WORKDIR="$(mktemp -d)"

docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
docker volume rm -f "$VOLUME_NAME" >/dev/null 2>&1 || true

# Nothing is published. GitLab's runner hands jobs the runner *host's*
# Docker socket, so a -p port binds on the host and the job container
# cannot reach it (.gitlab-ci.yml, test:container). The session and the
# health wait therefore run inside a throwaway container joined to this
# container's own network namespace, which behaves identically here and
# there.
RUN_ARGS=(-d --name "$CONTAINER_NAME" -v "$VOLUME_NAME:/var/lib/mikroview")

if $NEEDS_HISTORY_KEY; then
  # >= 32 bytes, per docs/configuration.md's history.keyFile section.
  # Not a real secret: it exists only to make this one throwaway
  # recording's fake accounts/flags/watchlist legible to the fixture
  # tarball's own key file, packed alongside the data directory below
  # (never committed -- see the .gitignore entry this script's own
  # header points at). 644 because rootless docker maps container uid 0
  # to the invoking user, and the image runs as uid 65532.
  head -c 32 /dev/urandom | base64 > "$WORKDIR/history.key"
  chmod 644 "$WORKDIR/history.key"
  RUN_ARGS+=(-v "$WORKDIR/history.key:/etc/mikroview/history.key:ro"
    -e MIKROVIEW_HISTORY_KEY_FILE=/etc/mikroview/history.key)
fi

if [ "$MODE" = "postgres" ]; then
  PG_DB="mikroview"
  PG_USER="mikroview"
  # Throwaway, generated per run, never written to a tracked file and
  # never passed in argv (an --env-file, so it stays out of the host's
  # process listing). The database it opens lives inside one container
  # for about a minute and is destroyed with it; pg_dump of a single
  # database carries no role passwords, and the dump is checked for this
  # one before it is uploaded regardless.
  PG_PASSWORD="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  umask 077
  {
    echo "POSTGRES_DB=${PG_DB}"
    echo "POSTGRES_USER=${PG_USER}"
    echo "POSTGRES_PASSWORD=${PG_PASSWORD}"
  } > "$WORKDIR/pg.env"

  docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
  docker network create "$NETWORK_NAME" >/dev/null

  # TLS on, with a throwaway self-signed certificate, because
  # persist.OpenPool refuses sslmode=disable/allow/prefer outright -- it
  # will not connect to a plaintext server at all. Same entrypoint
  # override .gitlab-ci.yml's test:postgres service uses.
  log "starting $PG_IMAGE with TLS"
  docker run -d --name "$PG_CONTAINER" \
    --network "$NETWORK_NAME" --network-alias postgres \
    --env-file "$WORKDIR/pg.env" \
    --entrypoint sh "$PG_IMAGE" -c '
      apk add --no-cache openssl >/dev/null
      mkdir -p /certs
      openssl req -new -x509 -days 1 -nodes -subj "/CN=postgres" \
        -keyout /certs/server.key -out /certs/server.crt >/dev/null 2>&1
      chown postgres:postgres /certs/server.crt /certs/server.key
      chmod 600 /certs/server.key && chmod 644 /certs/server.crt
      exec docker-entrypoint.sh postgres -c ssl=on \
        -c ssl_cert_file=/certs/server.crt -c ssl_key_file=/certs/server.key
    ' >/dev/null

  PG_READY=0
  for _ in $(seq 1 90); do
    if docker exec "$PG_CONTAINER" pg_isready -U "$PG_USER" -d "$PG_DB" >/dev/null 2>&1; then
      PG_READY=1
      break
    fi
    sleep 1
  done
  if [ "$PG_READY" != "1" ]; then
    log "postgres never became ready; last 40 log lines:"
    docker logs "$PG_CONTAINER" 2>&1 | tail -40 >&2
    exit 1
  fi
  log "postgres ready"

  # sslmode=require rather than verify-full: the certificate above is
  # generated seconds earlier inside a container on a private network
  # that nothing else can reach, so there is no chain to verify against.
  # What the mode buys here is that the released image's own TLS
  # enforcement is exercised rather than bypassed.
  printf '%s' "postgres://${PG_USER}:${PG_PASSWORD}@postgres:5432/${PG_DB}?sslmode=require" \
    > "$WORKDIR/postgres.dsn"
  chmod 644 "$WORKDIR/postgres.dsn"
  umask 022
  RUN_ARGS+=(--network "$NETWORK_NAME"
    -v "$WORKDIR/postgres.dsn:/etc/mikroview/postgres.dsn:ro"
    -e MIKROVIEW_POSTGRES_DSN_FILE=/etc/mikroview/postgres.dsn)
fi

log "starting $IMAGE"
docker run "${RUN_ARGS[@]}" "$IMAGE" >/dev/null

log "driving the scripted session"
cp "$ROOT/scripts/upgrade-fixture-session.py" "$WORKDIR/session.py"
set +e
docker run --rm --network "container:${CONTAINER_NAME}" -v "$WORKDIR:/w" "$HELPER_IMAGE" \
  sh -c "apk add --no-cache python3 >/dev/null && exec python3 /w/session.py \
    https://127.0.0.1:8080 127.0.0.1 ${SYSLOG_PORT} ${GENERATION} ${VERSION} ${SYSLOG_MODE}" \
  > "$WORKDIR/session.json"
SESSION_OK=$?
set -e
if [ "$SESSION_OK" -ne 0 ] || [ ! -s "$WORKDIR/session.json" ]; then
  log "scripted session failed; last 40 container log lines:"
  docker logs "$CONTAINER_NAME" 2>&1 | tail -40 >&2
  exit 1
fi
log "session recorded: $(cat "$WORKDIR/session.json")"

log "stopping container (graceful, so write-behind stores flush)"
docker stop --time 20 "$CONTAINER_NAME" >/dev/null

mkdir -p "$FIXTURE_DIR"

# forbid_real_addresses <path> -- refuses to keep a recording that holds
# a real-looking address or the owner's own hostname. See the
# generation-A/B/C comment above for the documentation ranges this
# script uses instead.
REAL_ADDRESS_RE='192\.168\.|(^|[^0-9])10\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}|tomlawson'
forbid_real_addresses() {
  if grep -rEq "$REAL_ADDRESS_RE" "$1"; then
    log "refusing to keep: a real-looking address or hostname pattern was found in $1"
    grep -rEn "$REAL_ADDRESS_RE" "$1" >&2 || true
    exit 1
  fi
}

if [ "$MODE" = "postgres" ]; then
  # pg_dump from inside the server's own container, so the client is the
  # same major version as the server. --no-owner --no-privileges because
  # the test restores into a database owned by a different role; the
  # dump is one database's contents, never roles or globals, so no
  # credential can be in it.
  log "dumping the database"
  docker exec "$PG_CONTAINER" pg_dump -U "$PG_USER" -d "$PG_DB" \
    --no-owner --no-privileges > "$WORKDIR/dump.sql"

  for table in store_blob schema_version; do
    grep -q "CREATE TABLE public.${table}" "$WORKDIR/dump.sql" || {
      log "refusing to keep: the dump has no ${table} table -- did the image really use Postgres?"
      exit 1
    }
  done
  grep -q "^COPY public.store_blob" "$WORKDIR/dump.sql" || {
    log "refusing to keep: the dump has no store_blob rows -- the session wrote nothing to the database"
    exit 1
  }
  if grep -qF "$PG_PASSWORD" "$WORKDIR/dump.sql"; then
    log "refusing to keep: the throwaway database password appears in the dump"
    exit 1
  fi
  forbid_real_addresses "$WORKDIR/dump.sql"

  gzip -9 -c "$WORKDIR/dump.sql" > "$DUMP"
  log "wrote $DUMP ($(du -h "$DUMP" | cut -f1))"

  if [ "${MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD:-}" = "1" ]; then
    log "MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD=1 set -- not uploading"
    exit 0
  fi
  log "uploading to the generic package registry (package upgrade-fixtures/$VERSION)"
  upload "$DUMP" "upgrade-fixture-${VERSION}-postgres.sql.gz"
  log "done"
  exit 0
fi

log "packing the data directory"
STAGE="$WORKDIR/stage"
mkdir -p "$STAGE/data"
# The volume's files are owned by uid 65532 (the image's own runtime
# user -- see Dockerfile's USER line at every one of these tags), which
# `cp -a` carries over as-is. Rootless docker maps container uid 0 to
# this host's own invoking user, so chowning to 0:0 before copying out
# is what makes the copy readable (and later removable) outside the
# container -- without it every later step here fails with "permission
# denied" reading its own temp directory.
docker run --rm -v "${VOLUME_NAME}:/data:ro" -v "$STAGE/data:/out" "$HELPER_IMAGE" \
  sh -c 'cp -a /data/. /out/ && chown -R 0:0 /out'
if $NEEDS_HISTORY_KEY; then
  cp "$WORKDIR/history.key" "$STAGE/history.key"
fi

forbid_real_addresses "$STAGE"

tar -C "$STAGE" -czf "$TARBALL" .
log "packed $TARBALL ($(du -h "$TARBALL" | cut -f1))"

mkdir -p "$MANIFEST_DIR"
python3 -c '
import json, sys
manifest = json.loads(sys.argv[1])
manifest["historyKeyEncrypted"] = sys.argv[2] == "true"
print(json.dumps(manifest, indent=2, sort_keys=True))
' "$(cat "$WORKDIR/session.json")" "$NEEDS_HISTORY_KEY" > "$MANIFEST_DIR/manifest.json"
log "wrote $MANIFEST_DIR/manifest.json"

if [ "${MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD:-}" = "1" ]; then
  log "MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD=1 set -- not uploading"
  exit 0
fi

log "uploading to the generic package registry (package upgrade-fixtures/$VERSION)"
upload "$TARBALL" "upgrade-fixture-${VERSION}.tar.gz"
# The manifest travels with the recording as well as being committed
# (#1247's decision, 2026-09-16): a job token can upload a package but
# cannot open a merge request, so the tag job cannot commit it. The test
# prefers the committed copy and falls back to this one, which makes
# committing it a review step rather than something the gate waits for.
upload "$MANIFEST_DIR/manifest.json" "manifest.json"
log "done"
