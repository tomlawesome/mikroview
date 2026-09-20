# Installing and running MikroView

The README's quickstart is the one-line form of this; this page is the full one.

## Prebuilt image

```sh
docker pull ghcr.io/tomlawesome/mikroview:latest
```

### The no-script form

The README's quickstart (`curl ... | sh`) is
[`install.sh`](https://github.com/tomlawesome/mikroview/blob/main/install.sh)
running this same `docker run`. If you'd rather run it yourself instead
of fetching a script:

```sh
docker run -d --name mikroview --restart unless-stopped \
    --read-only --cap-drop ALL --security-opt no-new-privileges --pids-limit 128 \
    -p 6514:6514/tcp -p 443:8080 \
    -v mikroview-data:/var/lib/mikroview \
    -v mikroview-etc:/etc/mikroview:ro \
    ghcr.io/tomlawesome/mikroview:latest
```

The `mikroview-etc` volume is the app folder #1243 introduced: an empty
folder is fine, and dropping a config file, GeoIP database or
certificate pair into it is picked up at the next restart with no other
change. It is mounted read-only, as the Compose examples below have
always mounted it -- MikroView only ever reads it, and nothing that
breaks into the container gets to rewrite your config or your keys. So
put files in from outside the app's own container, with either a helper
container:

```sh
docker run --rm -v mikroview-etc:/etc/mikroview -v "$PWD:/from:ro" \
    alpine cp /from/config.yaml /etc/mikroview/config.yaml
```

or by swapping the named volume for a bind mount of a folder on the
host, which is what the Compose examples do. `docker cp` into the
running container does not work here, by design: Docker refuses it with
"mounted volume is marked read-only". That
relies on nothing naming a config path explicitly, which is true of the
`docker run` above but **not** of the Compose example below -- see the
note under it. See "Persistent data" below.

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
      # One app folder on the host, two mounts: read-only for what you
      # own (config, keys, certs, GeoIP), read-write for what MikroView
      # owns. Layout and the ruling behind it: docs/decisions/app-folder.md.
      - ./mikroview:/etc/mikroview:ro
      - ./mikroview/data:/var/lib/mikroview
      # Mounting each file separately instead -- config.yaml,
      # GeoLite2-Country.mmdb, a bind-mounted data/ of its own, or a
      # named volume for data -- is the old way and still works: a path
      # set in config.yaml or the environment always wins over the
      # folder default. See docs/configuration.md.
    environment:
      # Not needed: MikroView finds ./mikroview/config.yaml on its own
      # and works fine without it too (defaults alone are a working
      # deployment). Naming it here explicitly would make that file
      # mandatory instead of optional -- see docs/configuration.md.
      # - MIKROVIEW_CONFIG=/etc/mikroview/config.yaml
      # Naming MIKROVIEW_GEOIP_DB_PATH explicitly instead of dropping the
      # file into mikroview/ is the old way and still works -- see
      # docs/configuration.md.
      # Only needed to move a store somewhere other than the default
      # /var/lib/mikroview/*.json -- see docs/configuration.md.
      # - MIKROVIEW_FLAGS_STORE_PATH=/var/lib/mikroview/flags.json
      # - MIKROVIEW_AUTH_STORE_PATH=/var/lib/mikroview/users.json
      # - MIKROVIEW_DEVICE_MAC_STORE_PATH=/var/lib/mikroview/mac-registry.json
      # TLS is on by default and needs no configuration to work -- these
      # are only for customizing it.
      # - MIKROVIEW_TLS_STORE_PATH=/var/lib/mikroview/tls
      # - MIKROVIEW_TLS_HOSTS=192.168.1.50,mikroview.local
      # Naming MIKROVIEW_TLS_CERT_FILE/MIKROVIEW_TLS_KEY_FILE explicitly
      # instead of dropping certs/tls.crt + certs/tls.key into mikroview/
      # is the old way and still works -- see docs/configuration.md.
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
```

**Image tags**: `latest` is the tag intended for general use -- the most recent release, promoted from `preview` after passing CI and a container smoke test. Every release is also published under its own immutable tag (`ghcr.io/tomlawesome/mikroview:v0.5.1`, matching the `v*` git tag and the [CHANGELOG](../CHANGELOG.md)), if you would rather pin one. `preview-<7-char-sha>` tags exist for every build off the `preview` branch, if you ever want to pin to a specific one rather than track `latest`. There is deliberately no `dev` tag: pushing to the `dev` branch never triggers a build at all, so nothing publishes from it -- if you ever see one referenced anywhere (including in your own `docker images` history), treat it as stale rather than a live channel, since nothing keeps it current.

Create the `mikroview` folder next to the compose file first, with your config inside it (see [`deploy/config.example.yaml`](../deploy/config.example.yaml) for the full option reference):

```
mikroview/
  config.yaml                   optional -- defaults run without it
  GeoLite2-Country.mmdb         optional -- country flags appear when present
  keys/history.key              optional -- history encryption on when present
  certs/tls.crt, certs/tls.key  optional -- your own certificate instead of the self-signed one
  data/                         MikroView's store; created for you
```

The bare `docker run` in the README's quickstart runs on defaults with a named volume and no folder at all, so there is nothing to set up for a first try there. **The Compose form is no different**: neither this example nor `deploy/docker-compose.yml` sets `MIKROVIEW_CONFIG`, so the app folder's own `config.yaml` is what decides, and an empty folder starts on defaults just as the `docker run` above does. Copy `deploy/config.example.yaml` in as `config.yaml` when you want to change something -- you do not need it to start. This follows the same shape as [`deploy/docker-compose.yml`](../deploy/docker-compose.yml) -- same ports, hardening and app-folder mount, local `build:` swapped for the prebuilt `image:` -- but leaves out the RouterOS-backup port and the less commonly moved store-path variables; see that file itself for the complete, fully-commented version.

## From source

```sh
mkdir -p deploy/mikroview
cp deploy/config.example.yaml deploy/mikroview/config.yaml
# edit deploy/mikroview/config.yaml with your router(s)' names/IPs, then:
chmod 644 deploy/mikroview/config.yaml

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

The README's bare `docker run` mounts a **named volume** over
`/var/lib/mikroview` -- where flags, local accounts, detector on/off
toggles, the new-device detector's MAC registry, and the self-generated
TLS certificate all persist. That no-folder default is deliberate: once
you've set up authentication or have flags worth keeping, losing them
on every `docker compose down` or image update -- not just a plain
restart -- would be a bad surprise, not an edge case. Docker populates
a fresh named volume from the image's own `/var/lib/mikroview` on first
use, ownership included, so there's no setup step needed. The tradeoff
is that you can't `cat`/`cp`/back up the files directly from the host --
you'd go through `docker run --rm -v mikroview-data:/data ...` or
`docker cp` instead.

Both compose files above mount `mikroview/data/`, inside the app
folder, instead -- a plain bind mount, so you can browse and back the
files up directly from the host. It needs one extra step first:
MikroView runs as a fixed non-root user inside the container, **uid
`1000`, gid `1000`** (the same identity used by the `--chown` in the
Dockerfile, and the first account on most Linux hosts -- likely you),
which can't chown a host directory the way a root-run container could.
Pick one before starting:

```sh
# Preferred: exact uid/gid ownership, nothing broader
mkdir -p mikroview/data
sudo chown 1000:1000 mikroview/data
```

```sh
# No root available on the host: open it up to everyone instead
mkdir -p mikroview/data
chmod 777 mikroview/data
```

If you'd rather have the named volume's opaque, no-setup persistence
instead of the bind-mounted folder, swap
`./mikroview/data:/var/lib/mikroview` in the compose file for a named
volume of your own -- naming paths explicitly like this is the old way
and still works exactly as before; see docs/decisions/app-folder.md.

The same fix applies to any other host path you bind-mount over a
`/var/lib/mikroview/*` sub-path (e.g. `flags.storePath`, `auth.storePath`,
`tls.storePath` — see [docs/configuration.md](configuration.md)), or
over `/etc/mikroview/GeoLite2-Country.mmdb` if you ever mount that
read-write. Read-only mounts like `config.yaml` don't need either fix —
world-readable (`chmod 644`, as in the Quickstart above) is enough, since
the container only needs to read it, not own it.

If a bind mount is misconfigured (wrong ownership), MikroView refuses to
start rather than running on it: it logs which store and directory
failed, the uid/gid it is running as versus the directory's actual
owner, and the exact `chown` that fixes it, then exits. Under
`restart: unless-stopped` that means a restart loop, not silent data
loss -- fix the ownership and the next restart comes up clean.
