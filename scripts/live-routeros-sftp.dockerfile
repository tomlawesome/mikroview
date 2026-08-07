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
CMD ["/usr/sbin/sshd", "-D", "-e"]
