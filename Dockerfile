# --- frontend build -------------------------------------------------------
FROM node:26-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# --- backend build ----------------------------------------------------------
FROM golang:1.27.0-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/dist ./web/dist
# VERSION is passed by .github/workflows/docker.yml as the short commit
# SHA it's building from -- stamped into the binary so it can identify
# itself at boot (see main.go's version var and logVersionAndMigration).
# Left at its default for a plain `docker build` with no --build-arg,
# which falls back to main.go's own "dev" default.
ARG VERSION=dev:local
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/mikroview .
# Every optional persistence path (flags/detector-settings/auth/TLS --
# see internal/config's DefaultDataDir) defaults under here, so it needs
# to exist and be owned by the runtime user out of the box. Created here
# rather than in the final stage: distroless has no shell, so it has no
# way to run `mkdir` itself -- this empty, correctly-owned directory is
# just copied in below.
RUN mkdir -p /var/lib/mikroview
# The mount point `-migrate-data` copies *into* (#537), and it exists for
# the same reason as the directory above: Docker seeds a fresh named
# volume from whatever the image has at that path, ownership included, so
# a volume mounted somewhere the image never created lands root-owned and
# uid 65532 cannot write a byte to it. Confirmed rather than assumed --
# `docker run --user 65532 -v newvol:/mnt/anywhere` gives a 0755 root:root
# directory and "Permission denied". Migrating bind mount -> volume is
# half of what #537 promises, so it needs a path where a brand new volume
# arrives already owned by the runtime user.
RUN mkdir -p /var/lib/mikroview-migrate

# --- runtime ------------------------------------------------------------
# distroless nonroot: uid 65532 can't bind ports <1024, which is why the
# app's own HTTP ports are 8080/8081 and docker-compose.yml maps the
# conventional 443/80 to them. Syslog needs no such remap: mikroview's
# only syslog listener is RouterOS remote-protocol=tls on 6514, which is
# already unprivileged.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/mikroview /mikroview
COPY --from=backend --chown=65532:65532 /var/lib/mikroview /var/lib/mikroview
COPY --from=backend --chown=65532:65532 /var/lib/mikroview-migrate /var/lib/mikroview-migrate
# Numeric, not the "nonroot" name. Same user either way -- distroless
# resolves nonroot to 65532 -- but the number is verifiable without
# reading the image's own /etc/passwd, which is what an orchestrator
# checking runAsNonRoot, or an operator reading `docker inspect`, has to
# do. It also matches the --chown just above and the uid
# docs/configuration.md tells operators to chown their DSN file to, so
# there is one number in play rather than a name and a number that a
# reader has to know are the same thing. See #285's supply-chain notes.
USER 65532:65532
# 8080/tcp serves HTTPS by default (TLS is on unless tls.enabled: false
# -- see docs/configuration.md's "TLS" section). 8081/tcp is a
# redirect-only listener that bounces plain HTTP to HTTPS -- see
# Listen.HTTPRedirect's doc comment in internal/config/config.go --
# docker-compose.yml maps the conventional host ports 443/80 to these.
# 6514/tcp is RouterOS remote-protocol=tls (RFC 5425's syslog-over-TLS
# port, already >1024 so no remap is needed); only started while
# tls.enabled is true.
EXPOSE 6514/tcp 8080/tcp 8081/tcp
# No shell/curl/wget in this image, so the binary checks itself -- see
# runHealthcheck in main.go.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/mikroview", "-healthcheck"]
ENTRYPOINT ["/mikroview"]
