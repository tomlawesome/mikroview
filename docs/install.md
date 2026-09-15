# Installing and running MikroView

The README's quickstart is the one-line form of this; this page is the full one.

## Prebuilt image

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
      # RouterOS remote-protocol=tls (RFC 5425's syslog-over-TLS port,
      # already unprivileged so no remap is needed). MikroView's only
      # syslog listener -- comment this out (and set
      # MIKROVIEW_LISTEN_SYSLOG_TLS= below) only if you want no syslog
      # ingest at all.
      - "6514:6514/tcp"
      # HTTPS by default, on the conventional port -- see the Quickstart
      # above and docs/configuration.md's "TLS" section.
      - "443:8080"
      # A plain-HTTP listener that only ever redirects to the HTTPS
      # port above -- never serves real content.
      - "80:8081"
    # Container hardening -- defense in depth on top of the image
    # already being distroless + non-root. MikroView binds only
    # unprivileged ports inside the container (which is why the
    # mappings above exist), so it needs no capabilities at all, and
    # every path it writes to is a mount -- see "Persistent data".
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    read_only: true
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
      # Disables syslog ingest entirely -- MikroView's only syslog
      # listener is RouterOS remote-protocol=tls, so only set this if
      # you don't want firewall events at all. Remove the "6514:6514/tcp"
      # port mapping above too if you do.
      # - MIKROVIEW_LISTEN_SYSLOG_TLS=

volumes:
  mikroview-data:
```

**Image tags**: `latest` is the tag intended for general use -- the most recent release, promoted from `preview` after passing CI and a container smoke test. Every release is also published under its own immutable tag (`ghcr.io/tomlawesome/mikroview:v0.5.1`, matching the `v*` git tag and the [CHANGELOG](../CHANGELOG.md)), if you would rather pin one. `preview-<7-char-sha>` tags exist for every build off the `preview` branch, if you ever want to pin to a specific one rather than track `latest`. There is deliberately no `dev` tag: pushing to the `dev` branch never triggers a build at all, so nothing publishes from it -- if you ever see one referenced anywhere (including in your own `docker images` history), treat it as stale rather than a live channel, since nothing keeps it current.

Create `config.yaml` next to it first (see [`deploy/config.example.yaml`](../deploy/config.example.yaml) for the full option reference), then `docker compose up -d`. This mirrors [`deploy/docker-compose.yml`](../deploy/docker-compose.yml) exactly, just swapping the local `build:` for the prebuilt `image:`.

## From source

```sh
cp deploy/config.example.yaml deploy/config.yaml
# edit deploy/config.yaml with your router(s)' names/IPs, then:
chmod 644 deploy/config.yaml

cd deploy
docker compose up -d --build
```

Then follow [docs/routeros-setup.md](routeros-setup.md) to point
your RouterOS device(s) at the container, and open
`https://<docker-host>` (port 443). A plain `http://<docker-host>`
request on port 80 redirects there automatically. MikroView serves TLS
by default with a
self-generated certificate (see [docs/configuration.md](configuration.md#tls)),
so your browser will show an untrusted-certificate warning on first
visit until you import that certificate -- expected for a self-hosted
admin interface with no external CA, same as Proxmox/TrueNAS/pfSense's
own web UIs.

## Persistent data

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
the named volume -- but it needs one extra step first: MikroView runs
as a fixed non-root user inside the container, **uid `1000`, gid
`1000`** (the same identity used by the `--chown` in the Dockerfile,
and the first account on most Linux hosts -- likely you), which can't
chown a host directory the way a root-run container could. Pick one
before starting:

```sh
# Preferred: exact uid/gid ownership, nothing broader
mkdir -p data
sudo chown 1000:1000 data
```

```sh
# No root available on the host: open it up to everyone instead
mkdir -p data
chmod 777 data
```

The same fix applies to any other host path you bind-mount over a
`/var/lib/mikroview/*` sub-path (e.g. `flags.storePath`, `auth.storePath`,
`tls.storePath` — see [docs/configuration.md](configuration.md)), or
over `/etc/mikroview/GeoLite2-Country.mmdb` if you ever mount that
read-write. Read-only mounts like `config.yaml` don't need either fix —
world-readable (`chmod 644`, as in the Quickstart above) is enough, since
the container only needs to read it, not own it.

If a bind mount is misconfigured (wrong ownership), MikroView logs
`permission denied` at startup and falls back to in-memory-only state
rather than crashing — annoying (you lose flags/accounts/TLS cert on
every restart) but not fatal.
