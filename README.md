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
curl -fsSL https://raw.githubusercontent.com/tomlawesome/mikroview/main/install.sh | sh
```

It pulls the image. It starts one container with two named volumes (your
data, and the app folder) and two ports. It prints the address to open.
Running it again is the upgrade line: same volumes, latest image. The
script is short enough to read before you run it —
[`install.sh`](https://github.com/tomlawesome/mikroview/blob/main/install.sh)
on GitHub.

Open `https://<docker-host>`, create the admin account, and the setup
wizard writes every RouterOS command with your values already filled in
— syslog over TLS, RouterOS 7.18+. Paste it into the router once and the
picture builds itself. The browser warns about the self-signed
certificate on first visit, as any self-hosted admin interface does;
[docs/configuration.md](docs/configuration.md#tls) covers bringing your
own.

`latest` is the most recent release; each release is also tagged
`v<version>` if you would rather pin one (`sh -s -- v0.6.0`). The
no-script `docker run` form, the full Compose file, image tags and where
your data lives are in [docs/install.md](docs/install.md).

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
- [docs/development.md](docs/development.md) — running, feeding and
  testing a checkout

## Contributions

Not accepted: no pull requests, and there is no public bug tracker —
development happens on a private GitLab and this repository is its
mirror. Security problems are the exception: report them privately, see
[SECURITY.md](SECURITY.md#reporting-a-vulnerability). You are free to
fork under the AGPL.

## License

MikroView is free and open source under the
[GNU AGPL v3.0](LICENSE). You can use, modify and self-host it at no
cost. If you change it and offer it to others over a network, the AGPL
asks you to publish your changes under the same licence. There is no
other licence on offer.

MikroView is an independent, community project, not affiliated with or
endorsed by MikroTik. RouterOS is a trademark of MikroTikls SIA.
