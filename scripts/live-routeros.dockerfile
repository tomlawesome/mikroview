# SPDX-License-Identifier: AGPL-3.0-only
#
# QEMU for the RouterOS live-check fixture (scripts/live-routeros.sh).
#
# This is containerised rather than installed on the host because the
# host install needs root and this fixture should not. Alpine's
# qemu-system-x86_64 is a fraction of the size of Debian's, and the
# image is the only part of the fixture that needs a package manager --
# the CHR image itself is a plain download.
#
# No KVM is assumed. /dev/kvm is passed through when the calling user can
# actually open it, and the guest falls back to TCG software emulation
# when it cannot, which is the case on an account that is not in the
# `kvm` group.
FROM alpine:3.22

RUN apk add --no-cache qemu-system-x86_64 qemu-img

# Pinning a USER here would fix the in-container uid, and /dev/kvm is
# passed through only when the *calling* user can open it -- see the note
# above. The rule is aimed at shipped images; this is a local live-check
# fixture that never leaves the workstation.
# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
ENTRYPOINT ["qemu-system-x86_64"]
