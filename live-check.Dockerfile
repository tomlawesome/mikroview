# SPDX-License-Identifier: AGPL-3.0-only
#
# The image the live-check gate runs in on a CI runner.
#
# Not the product image -- that is ./Dockerfile, distroless and nonroot,
# and it ships. This one exists so `make live-check` can run somewhere
# other than a developer's host: it carries the Go toolchain, Node, and
# Playwright's browsers, which is everything the gate needs and nothing
# the product does.
#
# Why an image at all, rather than installing on the runner: a run that
# spends its first ten minutes on `npm ci` and a browser download is a
# run nobody waits for, and a runner that accumulates toolchains by hand
# drifts from what the gate assumes. Every version below is pinned to
# what this repo already pins elsewhere -- go.mod's toolchain, the
# product Dockerfile's Node major, frontend/package.json's Playwright --
# so a bump here is the same decision as a bump there, not a second one.
#
# Base images name a tag, never a @sha256 digest, per AGENTS.md: a digest
# freezes the base and stops it receiving the security patches published
# by moving the tag, and nothing here bumps digests automatically.
FROM node:26-bookworm

# The Go toolchain, checksummed. go.mod's `go` directive is the source of
# truth for this version; the tarball is fetched rather than layered from
# golang:1.27 because that image is Debian-based too and stacking two
# distributions to save one download is a worse trade than it looks.
ARG GO_VERSION=1.27.0
ARG GO_SHA256=675c26c449cbb18fc24b74650de1eabbae6e16f64326fd85a283fb3b58280685
RUN set -eux; \
    curl -fsSLo /tmp/go.tar.gz "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"; \
    echo "${GO_SHA256}  /tmp/go.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    rm /tmp/go.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

# ENV PATH covers ordinary commands, but a *login* shell rebuilds PATH
# from /etc/profile and drops it -- so `bash -lc 'go version'` fails in an
# image where `go version` works, which is a confusing way to lose ten
# minutes. Some CI executors run job scripts as login shells. Caught by
# smoke-testing this image rather than by reading it.
RUN printf 'export PATH=/usr/local/go/bin:$PATH\n' > /etc/profile.d/go.sh

# Playwright's browsers, installed at the version frontend/package.json
# pins. Installing from that exact version rather than using Microsoft's
# published playwright image is deliberate: the published tags track
# their own release cadence, so the image's browsers and the repo's
# playwright package drift apart, and a mismatch surfaces as "Executable
# doesn't exist" in the middle of a gate run rather than at build time.
#
# All three engines, not just Chromium, and --with-deps pulls the system
# libraries each needs. The scenarios drive Chromium today, so this is
# capacity the harness cannot yet use -- deliberately, because the gate's
# single most expensive blind spot is that it only ever sees one engine.
# #659 is the worked example: static style attributes that Chromium
# tolerates and Firefox refuses under this app's CSP shipped green
# through live-check, vitest and every screenshot, and were caught by the
# owner opening the app. An image that cannot run Firefox guarantees that
# class of defect stays invisible here.
#
# "webkit" is what Playwright ships on Linux for Safari's engine. It is
# not Safari: no Safari-specific chrome, no iOS, and Apple's own quirks
# above the engine are out of reach on any Linux host. It catches
# engine-level differences, which is most of what bites, and nothing
# beyond that should be claimed from it.
ARG PLAYWRIGHT_VERSION=1.62.1
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
RUN set -eux; \
    npx --yes "playwright@${PLAYWRIGHT_VERSION}" install --with-deps chromium firefox webkit; \
    chmod -R a+rX /ms-playwright

# The gate runs as an unprivileged user, and that is load-bearing rather
# than hygiene. mikroview's own container runs `USER nonroot:nonroot`, and
# this repo has already been caught out once by the difference: test:go
# failed on GitLab CI purely because it ran as root, and the fix was to
# drop privileges (docs/decisions/gitlab-ci-root-in-container-test-failure.md).
# live-check exercises TLS stores, file permissions and terminal checks,
# all of which behave differently for root, so a gate running as root
# would be testing a configuration that never ships.
#
# node:26-bookworm already provides uid 1000 as `node`.
USER node
WORKDIR /work

# A shell, so `docker run` without a command lands somewhere useful
# rather than in Node's REPL.
CMD ["/bin/bash"]
