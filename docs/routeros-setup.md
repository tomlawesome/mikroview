# RouterOS setup

MikroView never talks to RouterOS's API and needs no credentials on the
router. Instead, RouterOS pushes firewall log lines to MikroView over
syslog. This is a one-time configuration on each router you want to
monitor.

## 1. Point RouterOS at the container

Replace `203.0.113.10` with the IP of the Docker host running MikroView.
The container listens on the conventional syslog port 514 externally (see
`deploy/docker-compose.yml`), even though it runs on 1514 internally.

```
/system logging action add name=mikroview target=remote remote=203.0.113.10 remote-port=514 remote-protocol=udp
```

UDP is the default and matches how OPNsense-style live views typically
work — occasional loss under extreme burst is an acceptable trade for
near-zero overhead. If you'd rather have guaranteed delivery over a lossy
link, use TCP instead:

```
/system logging action add name=mikroview target=remote remote=203.0.113.10 remote-port=514 remote-protocol=tcp
```

## 2. Forward firewall log events to it

RouterOS tags firewall rule matches with both the `firewall` category and
`info` severity — forward both:

```
/system logging add topics=firewall,info action=mikroview
```

## 3. Tag your firewall rules

RouterOS firewall log lines don't say "accepted" or "dropped" — that's
only known from *which rule* logged the packet. MikroView decodes this
from the rule's `log-prefix`, using a compact convention:

```
<ACTION>|<rule-slug>|
```

- `A` = accept, `D` = drop, `R` = reject, `L` = log-only/passthrough
- `rule-slug` is a short, human-meaningful label (lowercase, hyphens)
- **the trailing `|` is required** — RouterOS concatenates the log-prefix
  directly onto the log message with no guaranteed separating space, so
  without a hard terminator there'd be no reliable way to tell where the
  prefix ends and the rest of the message begins. A prefix missing the
  trailing `|` won't be recognized at all — every event will show up as
  action "unknown" (`?`), un-colored, un-labeled.

**Keep the whole prefix to 15 characters or fewer** (trailing `|`
included). RouterOS's log formatter is known to corrupt/overwrite
adjacent fields once a log-prefix gets much past ~20 characters — 15
stays safely clear of that.

### The universal part: tag rules you already have

You almost certainly already have a firewall rule set. The only thing
that matters is adding `log=yes` and a `log-prefix` to the rules you
want visible — don't recreate your rules, edit them in place. Find the
rule number with `/ip firewall filter print`, then:

```
/ip firewall filter set <rule-number> log=yes log-prefix="A|wan-in|"
```

(`A` here because that example rule accepts traffic — use `D`/`R`/`L`
for a drop/reject/log-only rule instead.) Repeat for whichever rules you
want to see in the live view — your default drop rule and a couple of
accept rules is a good starting point.

Rules without a `log-prefix` (or without `log=yes` at all) still work —
they show up with action "unknown" and no rule label, since MikroView has
no way to know what RouterOS decided without one.

### If you're starting from a blank firewall

The rules below are an *illustrative example*, not a universal script —
they assume things like a `mgmt` address-list existing and specific
chain/interface naming that won't match your setup. Treat them as a
reference for the pattern (match condition + `action=` + `log=yes
log-prefix=...`), not something to paste in blind:

```
/ip firewall filter add chain=input connection-state=established,related action=accept log=yes log-prefix="A|est-rel|"
/ip firewall filter add chain=input connection-state=invalid action=drop log=yes log-prefix="D|invalid|"
/ip firewall filter add chain=input protocol=tcp dst-port=22 src-address-list=mgmt action=accept log=yes log-prefix="A|mgmt-ssh|"
/ip firewall filter add chain=input action=drop log=yes log-prefix="D|input-def|"
/ip firewall filter add chain=forward action=accept log=yes log-prefix="A|lan-wan|"
/ip firewall filter add chain=forward action=drop log=yes log-prefix="D|fwd-def|"
```

## 4. Verify

On the router, confirm entries are being generated and sent:

```
/log print where topics~"firewall"
```

Then check MikroView picked it up — either watch the live view in the
browser, or:

```
curl http://<mikroview-host>:8080/api/devices
```

A router that's sending traffic but isn't yet in `config.yaml` still
shows up here, labelled by its source IP with `"configured": false` —
that's how you find the IP to add. See [configuration.md](configuration.md).
