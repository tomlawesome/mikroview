> [!IMPORTANT]
> **Development disclosure:** MikroView was coded by Claude under human
> direction.

> [!WARNING]
> **Under heavy development:** MikroView is pre-1.0 and still changing
> shape. Not every feature described in the docs is finished, and some do
> not yet work as intended.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="brand/logo-lockup-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="brand/logo-lockup-light.svg">
    <img src="brand/logo-lockup-dark.svg" alt="MikroView" width="280">
  </picture>
</p>

See what your firewall just did. MikroView turns RouterOS's firewall log
into a live picture of your network: every boundary the router watches,
every connection accepted, dropped or rejected, and which rule decided
it. The router pushes; MikroView never logs in to it — no RouterOS
credentials, no API access, near-zero load on the router. One container,
one Go binary with the interface built in.

<p align="center">
  <img src="docs/screenshots/fall-dark.png" alt="The fall, MikroView's landing view: one column per traffic boundary the router watches, with marks pouring down as connections cross it" width="820" />
</p>

## Quickstart

```sh
docker run -d --name mikroview \
    -p 6514:6514/tcp -p 443:8080 \
    -v mikroview-data:/var/lib/mikroview \
    ghcr.io/tomlawesome/mikroview:latest
```

Open `https://<docker-host>`, create the admin account, and the setup
wizard writes every RouterOS command with your values already filled in
— syslog over TLS, RouterOS 7.18+. Paste it into the router once and the
picture builds itself. The browser warns about the self-signed
certificate on first visit, as any self-hosted admin interface does;
[docs/configuration.md](docs/configuration.md#tls) covers bringing your
own.

`latest` is the most recent release; each release is also tagged
`v<version>` if you would rather pin one. The full Compose file, image
tags and where your data lives are in [docs/install.md](docs/install.md).

## Docs

- [docs/install.md](docs/install.md) — Compose file, image tags,
  persistent data and bind-mount permissions
- [docs/routeros-setup.md](docs/routeros-setup.md) — the RouterOS side:
  syslog forwarding and the log-prefix convention
- [docs/reading-the-fall.md](docs/reading-the-fall.md) — how to read the
  fall, the landing view
- [docs/features.md](docs/features.md) — what MikroView does, and the
  authentication choices from local accounts to SSO
- [docs/configuration.md](docs/configuration.md) — config.yaml
  reference, environment variables, API
- [docs/upgrades.md](docs/upgrades.md) — what happens when you start
  a newer build on existing data, and how to go back
- [docs/security-by-design.md](docs/security-by-design.md) — the
  security properties the design commits to
- [SECURITY.md](SECURITY.md) — threat model, hardening and how to report
  a vulnerability
- [CONTRIBUTING.md](CONTRIBUTING.md) — branching model and local
  development setup

## License

MikroView is free and open source under the
[GNU AGPL v3.0](LICENSE). You can use, modify and self-host it at no
cost, including inside a business. If you want to bundle MikroView into
a commercial product, offer it as a hosted service, or ship changes
without publishing them, a commercial licence is available — see
[COMMERCIAL-LICENSE.md](COMMERCIAL-LICENSE.md).

MikroView is an independent, community project, not affiliated with or
endorsed by MikroTik. RouterOS is a trademark of MikroTikls SIA.
