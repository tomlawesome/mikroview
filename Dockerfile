# --- frontend build -------------------------------------------------------
FROM node:26-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# --- backend build ----------------------------------------------------------
FROM golang:1.26.5-alpine AS backend
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

# --- runtime ------------------------------------------------------------
# distroless nonroot: uid 65532 can't bind ports <1024, hence the app's
# internal syslog port is 1514, not 514 -- docker-compose.yml maps the
# conventional host port 514 to it.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/mikroview /mikroview
COPY --from=backend --chown=65532:65532 /var/lib/mikroview /var/lib/mikroview
USER nonroot:nonroot
# 8080/tcp serves HTTPS by default (TLS is on unless tls.enabled: false
# -- see docs/configuration.md's "TLS" section). 8081/tcp is a
# redirect-only listener that bounces plain HTTP to HTTPS -- see
# Listen.HTTPRedirect's doc comment in internal/config/config.go --
# docker-compose.yml maps the conventional host ports 443/80 to these.
# 6514/tcp is RouterOS remote-protocol=tls (RFC 5425's syslog-over-TLS
# port, already >1024 so no remap is needed); only started while
# tls.enabled is true.
EXPOSE 1514/udp 1514/tcp 6514/tcp 8080/tcp 8081/tcp
# No shell/curl/wget in this image, so the binary checks itself -- see
# runHealthcheck in main.go.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/mikroview", "-healthcheck"]
ENTRYPOINT ["/mikroview"]
