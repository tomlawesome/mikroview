> [!IMPORTANT]
> **Development disclosure:** MikroView was coded by Claude under human
> direction.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="brand/logo-lockup-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="brand/logo-lockup-light.svg">
    <img src="brand/logo-lockup-dark.svg" alt="MikroView" width="280">
  </picture>
</p>

A real-time firewall "live view" for RouterOS, in the spirit of
OPNsense's live view: see every connection attempt as it happens,
whether it was accepted, dropped, or rejected, and by which rule —
filterable by device, IP/CIDR, port, protocol, interface, or rule.

Ships as a single Docker container. RouterOS pushes firewall log lines
to it over syslog (no API access, no credentials, near-zero load on the
router); MikroView parses, stores, and streams them to a fast, dark,
dependency-light web UI.

<p align="center">
  <img src="docs/screenshots/live-view-dark.png" alt="MikroView live view showing accepted (green), dropped (amber), and rejected (red) RouterOS firewall connections" width="820" />
</p>

A one-page overview with the same screenshots lives in
[`site/`](site/index.html) (open `site/index.html` directly, or serve the
directory statically) — see [`brand/`](brand/BRANDING.md) for the logo
assets and color tokens behind it.

## Quickstart

```sh
cp deploy/config.example.yaml deploy/config.yaml
# edit deploy/config.yaml with your router(s)' names/IPs, then:
chmod 644 deploy/config.yaml

cd deploy
docker compose up -d --build
```

Then follow [docs/routeros-setup.md](docs/routeros-setup.md) to point
your RouterOS device(s) at the container, and open
`http://<docker-host>:8080`.

### Prebuilt image

Every push to `main` builds and publishes an image via GitHub Actions
(see `.github/workflows/docker.yml`):

```sh
docker pull ghcr.io/tomlawesome/mikroview:latest
```

If `docker pull` reports the image as not found/unauthorized, the GHCR
package is likely still set to private — open the package settings on
GitHub (repo → Packages → mikroview) and set visibility to public, or
`docker login ghcr.io` first with a PAT that has `read:packages` scope.

## How it works

- **Ingestion**: RouterOS forwards firewall log lines via
  `/system logging` over syslog (UDP or TCP). No polling, no RouterOS
  API access, no credentials — push-based and cheap for the router.
- **Parsing**: a RouterOS-specific parser decodes chain, action, rule
  label, interfaces, protocol, addresses/ports, and length from each
  log line. See [docs/routeros-setup.md](docs/routeros-setup.md) for the
  log-prefix convention that makes "accept vs. drop vs. reject" and the
  responsible rule visible at all.
- **Storage**: in-memory only, a fixed-capacity ring buffer windowed to
  a configurable retention period (default 24h) — no database.
- **Live updates**: a WebSocket pushes new events to the browser in
  real time; historical/filtered queries go through a REST endpoint
  against the retained buffer. See
  [docs/configuration.md](docs/configuration.md) for the API and the
  server/client filtering split.
- **UI**: Svelte, no component framework, dark professional theme,
  ~50KB JS bundle.
- **Deployment**: multi-stage Docker build, final image based on
  distroless `nonroot`, embeds the built frontend into the Go binary via
  `go:embed` — one process, one image, no auth (intended for a trusted
  LAN — see [docs/configuration.md](docs/configuration.md)).

## Development

Requires Go 1.23+ and Node 22+.

```sh
make dev-backend    # go run ., syslog on :1514, http on :8080
make dev-frontend   # vite dev server on :5173, proxies /api to :8080
make test           # go test ./... + svelte-check
make build           # full build: frontend -> web/dist -> single Go binary
make docker          # docker build -t mikroview .
```

Feed it fixture syslog lines without a real router, e.g.:

```sh
printf '<134>Jan 15 10:22:31 MikroTik A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60' \
  | nc -u -w1 127.0.0.1 1514
```

## Docs

- [docs/routeros-setup.md](docs/routeros-setup.md) — RouterOS-side
  configuration (syslog forwarding, log-prefix convention)
- [docs/configuration.md](docs/configuration.md) — config.yaml
  reference, env vars, API reference
- [brand/BRANDING.md](brand/BRANDING.md) — logo files, color tokens,
  how to regenerate the PNG exports

## Not (yet) included

Deliberately deferred rather than half-built: authentication (trusted-LAN
only by design), TLS syslog, multi-arch images, config hot-reload,
server-side WebSocket filtering, JSON export.
