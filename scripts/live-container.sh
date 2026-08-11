#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# live-container.sh -- stand up mikroview as the *container it ships as*,
# and hand back the same environment scripts/live-env.sh does, so every
# frontend/scripts/live-*.mjs scenario runs against it unchanged.
#
# Why this exists alongside live-env.sh rather than replacing it
# (#273 slice 1): live-env.sh builds and runs a local binary, so it never
# exercises the image, the hardening it runs under, or Postgres. All
# three are places behaviour can differ from a `go build`:
#
#   - the image is distroless with no shell, so anything shelling out
#     fails there and nowhere else;
#   - it runs read-only with ALL capabilities dropped, so a path that
#     writes outside its volume fails there and nowhere else;
#   - #262 made Postgres a genuine fork in behaviour rather than a
#     config detail, and live-env.sh only ever exercises JSON files.
#
# Usage, identical in shape to live-env.sh:
#
#   eval "$(scripts/live-container.sh up)"   # MV_URL, MV_USER, MV_PASS, MV_DIR
#   scripts/live-container.sh syslog 200 my-rule
#   scripts/live-container.sh logs
#   scripts/live-container.sh down
#
# MV_BACKEND=postgres selects the Postgres backend; anything else (the
# default) uses the JSON-file stores. MV_IMAGE overrides the image, so
# the same harness can validate a *published* preview image rather than
# a locally built one -- which is what #273 slice 3 needs.
set -eu

MV_IMAGE="${MV_IMAGE:-mikroview:e2e}"
MV_BACKEND="${MV_BACKEND:-file}"
MV_DIR="${MV_DIR:-/tmp/mikroview-container}"
MV_USER="${MV_USER:-admin}"
MV_PASS="${MV_PASS:-live-check-password}"

# Fixed names so `down` cleans up even after an interrupted run.
APP_NAME="mv-e2e-app"
PG_NAME="mv-e2e-pg"
NET_NAME="mv-e2e-net"
PG_IMAGE="mv-e2e-pg-tls"

HTTP_PORT="${MV_HTTP_PORT:-18443}"
SYSLOG_TLS_PORT="${MV_SYSLOG_TLS_PORT:-16514}"

# Published on loopback only. The container is the thing under test, not
# something to expose -- and binding the published ports to 127.0.0.1
# keeps that true even on a machine with a LAN address.
BIND="127.0.0.1"

CURL_TLS="-k"

log() { echo "live-container: $*" >&2; }

# build_image builds the image under test unless MV_IMAGE was supplied,
# in which case it is taken as given (a published tag, for slice 3).
build_image() {
  if [ -n "${MV_IMAGE_PREBUILT:-}" ]; then
    log "using prebuilt image $MV_IMAGE"
    return 0
  fi
  # --network=host: rootless docker's default bridge cannot reach the
  # network from a build step, so module and npm downloads fail without
  # it. Documented here because the failure it prevents looks like a
  # transient network problem rather than a configuration one.
  log "building $MV_IMAGE"
  docker build --network=host -t "$MV_IMAGE" . >"$MV_DIR/build.log" 2>&1 || {
    log "image build failed; see $MV_DIR/build.log"
    tail -20 "$MV_DIR/build.log" >&2
    return 1
  }
}

# start_postgres brings up a Postgres that actually speaks TLS.
#
# Not a plain `docker run postgres`: internal/persist refuses any
# connection that could fall back to plaintext (see its requireTLS), so a
# server with no certificate is refused at boot and the harness would be
# testing the refusal rather than the backend. Same one-line derived
# image ci.yml's postgres job builds, for the same reason.
start_postgres() {
  mkdir -p "$MV_DIR/pgtls"
  openssl req -new -x509 -days 1 -nodes -subj "/CN=$PG_NAME" \
    -keyout "$MV_DIR/pgtls/server.key" -out "$MV_DIR/pgtls/server.crt" >/dev/null 2>&1
  cat > "$MV_DIR/pgtls/Dockerfile" <<'EOF'
FROM postgres:18-alpine
COPY server.crt /certs/server.crt
COPY server.key /certs/server.key
RUN chown postgres:postgres /certs/server.crt /certs/server.key \
 && chmod 600 /certs/server.key && chmod 644 /certs/server.crt
EOF
  docker build --network=host -t "$PG_IMAGE" "$MV_DIR/pgtls" >>"$MV_DIR/build.log" 2>&1

  docker run -d --name "$PG_NAME" --network "$NET_NAME" \
    -e POSTGRES_PASSWORD=e2e -e POSTGRES_DB=mikroview \
    "$PG_IMAGE" -c ssl=on \
    -c ssl_cert_file=/certs/server.crt -c ssl_key_file=/certs/server.key >/dev/null

  for _ in $(seq 1 60); do
    if docker exec "$PG_NAME" pg_isready -U postgres -d mikroview >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  log "postgres did not become ready"
  docker logs "$PG_NAME" >&2
  return 1
}

up() {
  down >/dev/null 2>&1 || true
  rm -rf "$MV_DIR"
  mkdir -p "$MV_DIR"

  build_image || return 1

  docker network create "$NET_NAME" >/dev/null 2>&1 || true

  # The config lives on the host and is mounted read-only, exactly as
  # deploy/docker-compose.yml does it -- so the harness exercises the
  # same shape an operator deploys rather than an env-var-only variant
  # nothing ships.
  #
  # httpRedirect is off: the redirect listener is covered by its own unit
  # tests, and leaving it on would mean publishing a third port for
  # something no scenario drives.
  cat > "$MV_DIR/config.yaml" <<EOF
listen:
  http: "0.0.0.0:8080"
  httpRedirect: ""
  syslogTls: "0.0.0.0:6514"
tls:
  enabled: true
  hosts: ["127.0.0.1", "localhost"]
  storePath: /var/lib/mikroview/tls
auth:
  storePath: /var/lib/mikroview/users.json
  recoveryKeysPath: /var/lib/mikroview/recovery.json
  recoveryPepperPath: /var/lib/mikroview/pepper
  tokensStorePath: /var/lib/mikroview/tokens.json
  secureCookie: true
flags: {storePath: /var/lib/mikroview/flags.json}
entities: {storePath: /var/lib/mikroview/entities.json}
audit: {storePath: /var/lib/mikroview/audit.json}
watchlist:
  storePath: /var/lib/mikroview/watchlist.json
  matchLogPath: /var/lib/mikroview/matchlog.jsonl
EOF

  PG_ENV=""
  if [ "$MV_BACKEND" = "postgres" ]; then
    start_postgres || return 1
    # The DSN is read from a file, never argv or a config field, so it
    # never appears in `docker inspect` or a process list -- which is
    # the property internal/persist documents and this harness must not
    # quietly bypass.
    printf 'postgres://postgres:e2e@%s:5432/mikroview?sslmode=require' "$PG_NAME" > "$MV_DIR/pg.dsn"
    # 0644, not 0600 -- and this is worth explaining rather than just
    # doing, because getting it wrong is how this harness first failed:
    #
    #   ERROR storage │ postgres: reading DSN file /etc/mikroview/pg.dsn:
    #                   open /etc/mikroview/pg.dsn: permission denied
    #
    # The image runs as uid 65532, which is not the host user that wrote
    # the file, so a 0600 file mounted in is unreadable to it and
    # mikroview refuses to start. A real deployment should keep 0600 and
    # chown the file to 65532 (now documented in docs/configuration.md,
    # which did not say so). Here the credential is "e2e", against a
    # throwaway container on a private network, deleted when the run
    # ends -- so widening the mode is the simpler of the two and costs
    # nothing.
    chmod 644 "$MV_DIR/pg.dsn"
    PG_ENV="-e MIKROVIEW_POSTGRES_DSN_FILE=/etc/mikroview/pg.dsn"
  fi

  # Hardening deliberately matches ci.yml's container smoke test, plus
  # the read-only root and dropped capabilities compose ships. A
  # scenario that only passes with these relaxed is a real finding about
  # the shipped deployment, not a harness problem.
  # shellcheck disable=SC2086
  docker run -d --name "$APP_NAME" --network "$NET_NAME" \
    --read-only \
    --cap-drop ALL --security-opt no-new-privileges \
    --pids-limit 128 --memory 512m --cpus 1.0 \
    -v "$MV_DIR/config.yaml:/etc/mikroview/config.yaml:ro" \
    -v "$MV_DIR/pg.dsn:/etc/mikroview/pg.dsn:ro" \
    -v "$APP_NAME-data:/var/lib/mikroview" \
    -e MIKROVIEW_CONFIG=/etc/mikroview/config.yaml \
    $PG_ENV \
    -p "$BIND:$HTTP_PORT:8080" \
    -p "$BIND:$SYSLOG_TLS_PORT:6514" \
    "$MV_IMAGE" >/dev/null 2>&1 || {
      # The pg.dsn mount fails when the file does not exist (file
      # backend). Retry without it rather than requiring two near-
      # identical run lines.
      docker rm -f "$APP_NAME" >/dev/null 2>&1 || true
      docker run -d --name "$APP_NAME" --network "$NET_NAME" \
        --read-only \
        --cap-drop ALL --security-opt no-new-privileges \
        --pids-limit 128 --memory 512m --cpus 1.0 \
        -v "$MV_DIR/config.yaml:/etc/mikroview/config.yaml:ro" \
        -v "$APP_NAME-data:/var/lib/mikroview" \
        -e MIKROVIEW_CONFIG=/etc/mikroview/config.yaml \
        -p "$BIND:$HTTP_PORT:8080" \
        -p "$BIND:$SYSLOG_TLS_PORT:6514" \
        "$MV_IMAGE" >/dev/null
    }

  for _ in $(seq 1 60); do
    if curl -fsS $CURL_TLS "https://$BIND:$HTTP_PORT/api/healthz" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done
  if ! curl -fsS $CURL_TLS "https://$BIND:$HTTP_PORT/api/healthz" >/dev/null 2>&1; then
    log "container did not become healthy"
    docker logs "$APP_NAME" >&2
    return 1
  fi

  curl -fsS $CURL_TLS -X POST -H 'Content-Type: application/json' \
    -H 'X-Requested-With: mikroview' \
    -d "{\"username\":\"$MV_USER\",\"password\":\"$MV_PASS\"}" \
    "https://$BIND:$HTTP_PORT/api/auth/register" >/dev/null

  echo "export MV_URL=https://$BIND:$HTTP_PORT"
  echo "export MV_USER=$MV_USER"
  echo "export MV_PASS=$MV_PASS"
  echo "export MV_DIR=$MV_DIR"
  echo "export MV_SYSLOG_TLS_PORT=$SYSLOG_TLS_PORT"
}

# send_tls -- read complete syslog lines on stdin and deliver them over
# the container's published TLS listener. The single sender every feed
# shape below goes through, mirroring live-env.sh's structure exactly.
#
# One sender rather than one per shape, learned the hard way: the first
# version of this file gave `syslog`, `raw` and `portscan` their own
# copies, and the `raw` copy was the one that omitted TCP_NODELAY. A
# single small write then sat in the kernel buffer while the socket
# closed underneath it, so `raw` delivered *nothing* -- silently, with
# the send appearing to succeed. Bulk sends were unaffected, which is
# what made it look like a product bug in the two scenarios that use
# raw rather than a defect in the harness. Measured: 5 of 5 bulk events
# arrived, 0 of 1 raw.
#
# The pacing is load-bearing for the same reason live-env.sh documents:
# the listener hands each line to a buffered channel with a
# non-blocking send, so one bulk write outruns the consumer and loses
# events (575 of 900 before pacing, 900 of 900 after).
send_tls() {
  python3 -c '
import socket, ssl, sys, time
host, port = sys.argv[1], int(sys.argv[2])
lines = [l for l in sys.stdin.buffer.read().split(b"\n") if l]
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
with socket.create_connection((host, port), timeout=15) as sock:
    sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
    with ctx.wrap_socket(sock, server_hostname=host) as tls:
        for i, line in enumerate(lines):
            tls.sendall(line + b"\n")
            if i % 25 == 24:
                time.sleep(0.01)
' "$BIND" "$SYSLOG_TLS_PORT"
}

# syslog N [label] -- N synthetic firewall events.
syslog() {
  python3 - "${1:-100}" "${2:-live}" <<'PY' | send_tls
import sys
count, label = int(sys.argv[1]), sys.argv[2]
for i in range(count):
    action = "D" if i % 3 else "A"
    print(f"{action}|{label}|forward: in:ether1 out:bridge1, "
          f"connection-state:new src-mac aa:bb:cc:dd:ee:{i % 256:02x}, proto TCP (SYN), "
          f"192.168.1.{i % 254 + 1}:{40000 + i}->203.0.113.{i % 254 + 1}:443, len 60")
PY
}

# raw LINE -- one exact line, for a scenario needing a specific event
# shape rather than the bulk pattern.
raw() {
  printf '%s\n' "$1" | send_tls
}

# portscan N [source-ip] -- N distinct destination ports from one source
# inside the port-scan window, so a real port_scan flag is raised by the
# detector rather than synthesized.
portscan() {
  python3 - "${1:-20}" "${2:-198.51.100.77}" <<'PY' | send_tls
import sys
n, src = int(sys.argv[1]), sys.argv[2]
for i in range(n):
    print(f"firewall,info D|scan-src| forward: in:ether1 out:bridge1, "
          f"connection-state:new, proto TCP (SYN), "
          f"{src}:{40000+i}->192.168.1.10:{1000+i}, len 60")
PY
}

logs() { docker logs "$APP_NAME" 2>&1; }

down() {
  docker rm -f "$APP_NAME" >/dev/null 2>&1 || true
  docker rm -f "$PG_NAME" >/dev/null 2>&1 || true
  docker volume rm -f "$APP_NAME-data" >/dev/null 2>&1 || true
  docker network rm "$NET_NAME" >/dev/null 2>&1 || true
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  syslog) shift; syslog "$@" ;;
  raw) shift; raw "$@" ;;
  portscan) shift; portscan "$@" ;;
  logs) logs ;;
  *) echo "usage: $0 {up|down|syslog N [label]|raw LINE|portscan N [src-ip]|logs}" >&2; exit 2 ;;
esac
