# Quiet host (issue #1003)

One box runs every `ai` group runner and mikroview's dev gate loop. When
`perf:promotion` measures UI performance it needs the box quiet, so it
writes a flag file (`/srv/quiet-host/hold`) and this root-owned unit sets
`concurrent = 1` in `/etc/gitlab-runner/config.toml` until the flag goes
away or expires. No token of any kind is involved — everything here is a
flag file and a script, both owned by root.

Run each block below as root on that box (prefix every command with
`sudo`, or run the block in a root shell). The script keeps the saved
`concurrent` value in `/var/lib/quiet-host/`, which it creates itself.

Create the flag directory. The runner's containers run as `gitlab-runner`
under rootless Docker, and `mvagent` needs to read the flag too, so both
need access:

```
mkdir -p /srv/quiet-host
chown 100999:100999 /srv/quiet-host
chmod 0775 /srv/quiet-host
```

`100999`, not `gitlab-runner`. The runner's Docker is rootless (its socket
is `/run/user/988/docker.sock`), so the job's container runs in a user
namespace: host uid 988 appears inside as uid 0, and hosts 100000-165535
appear as 1-65536. `live-check.Dockerfile` ends `USER node`, uid 1000, so
the job writes as host uid 100000 + 999 = 100999. A directory owned by
`gitlab-runner` is not writable by it -- from inside the container that
directory shows as owned by 65534 (`nobody`), because the host uid is
outside the mapped range, and `mktemp` fails with "Permission denied".

This was wrong from the first install and `perf:promotion` could never
have worked: it failed here on every attempt until 2026-09-06 (#1003).
If the runner is ever moved off rootless Docker, or its subuid range
changes, this number changes with it -- check `/etc/subuid` for
`gitlab-runner` and add 999 to the start of its range.

Install the script:

```
cp quiet-host-apply.sh /usr/local/sbin/quiet-host-apply.sh
chmod 0755 /usr/local/sbin/quiet-host-apply.sh
```

Install the units:

```
cp quiet-host.path quiet-host.service quiet-host-expire.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now quiet-host.path quiet-host-expire.timer
```

Add the flag directory as a bind mount on the `big` runner (id 1) so jobs
running in its containers can see it. Edit
`/etc/gitlab-runner/config.toml`, in that runner's `[runners.docker]`
section, and add to `volumes`:

```
"/srv/quiet-host:/quiet-host"
```

## Verify it

Touch a hold flag by hand, with `expires` two minutes out:

```
echo "job=manual
url=
started=$(date +%s)
expires=$(($(date +%s) + 120))" > /srv/quiet-host/hold
```

Watch it apply:

```
journalctl -u quiet-host.service -f
```

You should see a `HOLD applied` line. Check the config changed:

```
grep ^concurrent /etc/gitlab-runner/config.toml
```

Remove the flag and watch it restore (the minutely timer catches it
within a minute even if the watcher misses the removal):

```
rm /srv/quiet-host/hold
```

The `HOLD applied` and restore lines should never show the rest of the
config file — only the `concurrent` line's value.

## Recovery

If the box looks stuck with `concurrent = 1` and nothing seems to be
measuring:

```
rm /srv/quiet-host/hold
```

`quiet-host-expire.timer` runs every minute regardless, so a hold with a
past `expires` is released within a minute even if this is never run by
hand.
