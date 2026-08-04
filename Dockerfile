# --- frontend build -------------------------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# --- backend build ----------------------------------------------------------
FROM golang:1.23.4-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mikroview .

# --- runtime ------------------------------------------------------------
# distroless nonroot: uid 65532 can't bind ports <1024, hence the app's
# internal syslog port is 1514, not 514 -- docker-compose.yml maps the
# conventional host port 514 to it.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/mikroview /mikroview
USER nonroot:nonroot
EXPOSE 1514/udp 1514/tcp 8080/tcp
# No shell/curl/wget in this image, so the binary checks itself -- see
# runHealthcheck in main.go.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/mikroview", "-healthcheck"]
ENTRYPOINT ["/mikroview"]
