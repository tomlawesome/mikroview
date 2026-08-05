#!/usr/bin/env bash
# Lists running Docker containers' published (host-bound) ports -- a
# companion to mikroview's port "i" lookup: cross-reference a port you see
# hit in the live view against what's actually running on this host.
# Not part of the shipped product -- a standalone dev-host helper, run
# directly on wherever Docker is running (not inside mikroview itself).
#
# Usage:
#   ./list-exposed-ports.sh              # print a table to stdout
#   ./list-exposed-ports.sh -o ports.txt # write the same table to a file
set -euo pipefail

output=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o|--output)
      output="${2:?missing path after $1}"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [-o|--output FILE]"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo "docker not found on PATH" >&2
  exit 1
fi

render() {
  printf '%-22s %-32s %-23s %-16s %s\n' "CONTAINER" "IMAGE" "HOST BINDING" "CONTAINER PORT" "EXPOSURE"

  local ids
  ids="$(docker ps -q)"
  if [[ -z "$ids" ]]; then
    echo "(no running containers)"
    return
  fi

  local id name image port_field
  for id in $ids; do
    # One inspect call per container, ranging over NetworkSettings.Ports
    # ourselves via a Go template -- avoids depending on jq being
    # installed on the host, since docker itself is the only thing this
    # script assumes is present.
    name="$(docker inspect -f '{{.Name}}' "$id" | sed 's#^/##')"
    image="$(docker inspect -f '{{.Config.Image}}' "$id")"
    port_field="$(docker inspect -f '{{range $port, $bindings := .NetworkSettings.Ports}}{{if $bindings}}{{range $bindings}}{{.HostIp}}|{{.HostPort}}|{{$port}}{{println}}{{end}}{{end}}{{end}}' "$id")"

    [[ -z "$port_field" ]] && continue # nothing published to the host -- not "exposing" anything

    while IFS='|' read -r host_ip host_port container_port; do
      [[ -z "$container_port" ]] && continue
      local binding="${host_ip:-0.0.0.0}:${host_port}"
      local exposure="public"
      if [[ "$host_ip" == "127.0.0.1" || "$host_ip" == "::1" ]]; then
        exposure="loopback-only"
      fi
      printf '%-22s %-32s %-23s %-16s %s\n' "$name" "$image" "$binding" "$container_port" "$exposure"
    done <<< "$port_field"
  done
}

if [[ -n "$output" ]]; then
  render > "$output"
  echo "Wrote $output"
else
  render
fi
