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
`https://<docker-host>` (port 443). A plain `http://<docker-host>`
request on port 80 redirects there automatically. mikroview serves TLS
by default with a
self-generated certificate (see [docs/configuration.md](docs/configuration.md#tls)),
so your browser will show an untrusted-certificate warning on first
visit until you import that certificate -- expected for a self-hosted
admin interface with no external CA, same as Proxmox/TrueNAS/pfSense's
own web UIs.

### Prebuilt image

```sh
docker pull ghcr.io/tomlawesome/mikroview:latest
```

Or run it directly with Compose, without cloning the repo:

```yaml
services:
  mikroview:
    image: ghcr.io/tomlawesome/mikroview:latest
    restart: unless-stopped
    ports:
      # Host 514 -> the conventional syslog port RouterOS targets; the
      # container listens on 1514 internally since it runs as a non-root
      # user that can't bind <1024 (see Dockerfile).
      - "514:1514/udp"
      - "514:1514/tcp"
      # HTTPS by default, on the conventional port -- see the Quickstart
      # above and docs/configuration.md's "TLS" section.
      - "443:8080"
      # A plain-HTTP listener that only ever redirects to the HTTPS
      # port above -- never serves real content.
      - "80:8081"
    healthcheck:
      test: ["CMD", "/mikroview", "-healthcheck"]
      interval: 30s
      timeout: 5s
      start_period: 10s
      retries: 3
    volumes:
      - ./config.yaml:/etc/mikroview/config.yaml:ro
      # Optional -- see docs/configuration.md's GeoIP section. Requires
      # your own MaxMind GeoLite2 database; uncomment both this and the
      # env var below once you have one.
      # - ./GeoLite2-Country.mmdb:/etc/mikroview/GeoLite2-Country.mmdb:ro
      # Persists flags/accounts/detector settings/the new-device MAC
      # registry/the TLS cert across container recreation, not just
      # restarts -- see "Persistent data" below for what this is and
      # the bind-mount alternative.
      - mikroview-data:/var/lib/mikroview
      # Alternative to the named volume above: a bind mount, if you want
      # to browse/back up the files directly from the host. Comment out
      # the line above and uncomment this one instead (not both) -- see
      # "Persistent data" below for the permissions step this needs
      # first.
      # - ./data:/var/lib/mikroview
    environment:
      - MIKROVIEW_CONFIG=/etc/mikroview/config.yaml
      # - MIKROVIEW_GEOIP_DB_PATH=/etc/mikroview/GeoLite2-Country.mmdb
      # Only needed to move a store somewhere other than the default
      # /var/lib/mikroview/*.json -- see docs/configuration.md.
      # - MIKROVIEW_FLAGS_STORE_PATH=/var/lib/mikroview/flags.json
      # - MIKROVIEW_FLAGS_DETECTOR_SETTINGS_STORE_PATH=/var/lib/mikroview/detector-settings.json
      # - MIKROVIEW_AUTH_STORE_PATH=/var/lib/mikroview/users.json
      # - MIKROVIEW_DEVICE_MAC_STORE_PATH=/var/lib/mikroview/mac-registry.json
      # TLS is on by default and needs no configuration to work -- these
      # are only for customizing it.
      # - MIKROVIEW_TLS_STORE_PATH=/var/lib/mikroview/tls
      # - MIKROVIEW_TLS_HOSTS=192.168.1.50,mikroview.local
      # - MIKROVIEW_TLS_CERT_FILE=/etc/mikroview/tls.crt
      # - MIKROVIEW_TLS_KEY_FILE=/etc/mikroview/tls.key
      # Read docs/configuration.md's "TLS" section before using this --
      # only safe if this port is unreachable except from your own
      # isolated-network reverse proxy.
      # - MIKROVIEW_TLS_ENABLED=false
      # Disables the plain-HTTP redirect-only listener above -- only
      # needed if you've removed the "80:8081" port mapping too (e.g.
      # your reverse proxy handles the HTTP->HTTPS redirect itself).
      # - MIKROVIEW_LISTEN_HTTP_REDIRECT=

volumes:
  mikroview-data:
```

Create `config.yaml` next to it first (see [`deploy/config.example.yaml`](deploy/config.example.yaml) for the full option reference), then `docker compose up -d`. This mirrors [`deploy/docker-compose.yml`](deploy/docker-compose.yml) exactly, just swapping the local `build:` for the prebuilt `image:`.

### Persistent data

By default, both compose files above mount a **named volume** over
`/var/lib/mikroview` -- where flags, local accounts, detector on/off
toggles, the new-device detector's MAC registry, and the self-generated
TLS certificate all persist. This is the default deliberately: once
you've set up authentication or have flags worth keeping, losing them
on every `docker compose down` or image update -- not just a plain
restart -- would be a bad surprise, not an edge case. Docker populates
a fresh named volume from the image's own `/var/lib/mikroview` on first
use, ownership included, so there's no setup step needed.

The tradeoff is that you can't `cat`/`cp`/back up the files directly
from the host the way you can with a bind mount -- you'd go through
`docker run --rm -v mikroview-data:/data ...` or `docker cp` instead.
If you want that direct host access, switch to the commented-out bind
mount in either compose file (`./data:/var/lib/mikroview`) instead of
the named volume -- but it needs one extra step first: mikroview runs
as a fixed non-root user inside the container, **uid `65532`, gid
`65532`** (distroless's built-in `nonroot` account, same identity used
by the `--chown` in the Dockerfile), which can't chown a host directory
the way a root-run container could. Pick one before starting:

```sh
# Preferred: exact uid/gid ownership, nothing broader
mkdir -p data
sudo chown 65532:65532 data
```

```sh
# No root available on the host: open it up to everyone instead
mkdir -p data
chmod 777 data
```

The same fix applies to any other host path you bind-mount over a
`/var/lib/mikroview/*` sub-path (e.g. `flags.storePath`, `auth.storePath`,
`tls.storePath` — see [docs/configuration.md](docs/configuration.md)), or
over `/etc/mikroview/GeoLite2-Country.mmdb` if you ever mount that
read-write. Read-only mounts like `config.yaml` don't need either fix —
world-readable (`chmod 644`, as in the Quickstart above) is enough, since
the container only needs to read it, not own it.

If a bind mount is misconfigured (wrong ownership), mikroview logs
`permission denied` at startup and falls back to in-memory-only state
rather than crashing — annoying (you lose flags/accounts/TLS cert on
every restart) but not fatal.

## Features

- **Ingestion**: RouterOS forwards firewall log lines via
  `/system logging` over syslog (UDP or TCP). No polling, no RouterOS
  API access, no credentials — push-based and cheap for the router.
- **Parsing**: a RouterOS-specific parser decodes chain, action, rule
  label, interfaces, protocol, addresses/ports, and length from each
  log line. See [docs/routeros-setup.md](docs/routeros-setup.md) for the
  log-prefix convention that makes "accept vs. drop vs. reject" and the
  responsible rule visible at all.
- **Storage**: in-memory only, a fixed-capacity ring buffer windowed to
  a configurable retention period (default 24h) — no database, and no
  disk persistence. All retained events are lost on restart, redeploy,
  or crash; MikroView is a live/recent-history view, not a log archive.
  The one deliberate exception is behavioral flags (see below), which
  can optionally persist to a small JSON file since they're meant to
  stay visible until a human clears them.
- **Behavioral flags**: watches for port scans, per-source activity
  spikes, repeated attempts against critical ports (SSH, RDP, Winbox,
  ...) from external IPs, and network-wide volume spikes — each raises a
  flag for a human to review and clear, never an automatic action. See
  [docs/configuration.md](docs/configuration.md) for the detectors and
  their thresholds.
- **Live updates**: a WebSocket pushes new events to the browser in
  real time; historical/filtered queries go through a REST endpoint
  against the retained buffer. See
  [docs/configuration.md](docs/configuration.md) for the API and the
  server/client filtering split.
- **UI**: Svelte, no component framework, dark professional theme,
  ~50KB JS bundle.
- **Logging**: leveled (debug/info/warn/error) and colorized server
  output, auto-plain when piped or `NO_COLOR` is set. See
  [docs/configuration.md](docs/configuration.md)'s "Logging" section.
- **Authentication**: a one-time choice on first load — create the admin
  account, or explicitly skip auth for this deployment. Creating an
  account makes it required for everything except the health check
  (Argon2id-hashed passwords, opaque server-side sessions,
  self-registration for the first/super-admin account only). Local
  accounts and single sign-on via an external OIDC identity provider
  (e.g. Authentik, Keycloak, Entra ID) can both be enabled at once. See
  [docs/configuration.md](docs/configuration.md) and
  [SECURITY.md](SECURITY.md) for the threat model and setup.

## Docs

- [docs/routeros-setup.md](docs/routeros-setup.md) — RouterOS-side
  configuration (syslog forwarding, log-prefix convention)
- [docs/configuration.md](docs/configuration.md) — config.yaml
  reference, env vars, API reference
- [brand/BRANDING.md](brand/BRANDING.md) — logo files, color tokens,
  how to regenerate the PNG exports
- [CONTRIBUTING.md](CONTRIBUTING.md) — branching model and local
  development setup, for anyone submitting a pull request

## License

MikroView is source-available, not open source: free to use and
modify for personal/non-commercial purposes, no redistribution or
commercial use without permission. See [LICENSE](LICENSE) for the
full terms.
