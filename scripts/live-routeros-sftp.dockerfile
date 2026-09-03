# SPDX-License-Identifier: AGPL-3.0-only
#
# A throwaway SFTP server, so the question "could the router upload a
# file instead of POSTing it?" is answered by watching a router try.
#
# It exists only inside scripts/live-routeros-step0.sh. mikroview does
# not ship an SFTP server and this is not a step towards one -- see
# docs/decisions/routeros-ingest-spike.md for what the experiment found
# and why the answer was no.
FROM alpine:3.22

RUN apk add --no-cache openssh-server openssh-sftp-server \
 && ssh-keygen -A \
 && adduser -D -s /sbin/nologin mvingest \
 && echo 'mvingest:ingest-pass-123' | chpasswd \
 && mkdir -p /drop && chown mvingest:mvingest /drop \
 && printf 'Subsystem sftp internal-sftp\nPasswordAuthentication yes\nPermitRootLogin no\nMatch User mvingest\n  ForceCommand internal-sftp\n' >> /etc/ssh/sshd_config

EXPOSE 22
# sshd has to start as root to bind the port and to drop to mvingest per
# connection, so a USER line here would stop the fixture working. The
# rule is aimed at shipped images; this one is built by
# scripts/live-routeros-step0.sh on a workstation and thrown away.
# nosemgrep: dockerfile.security.missing-user.missing-user
CMD ["/usr/sbin/sshd", "-D", "-e"]
