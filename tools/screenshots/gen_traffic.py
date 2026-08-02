#!/usr/bin/env python3
"""Feeds a realistic, varied stream of RouterOS-shaped syslog lines into a
running mikroview instance for screenshots/demos. Not part of the shipped
product -- a one-off dev tool."""
import random
import socket
import sys
import time

TARGET = ("127.0.0.1", int(sys.argv[1]) if len(sys.argv) > 1 else 1514)

DEVICES = {
    "core": "127.0.0.2",     # matches deploy config.yaml -> shows as "Core Router"
    "branch": "127.0.0.3",   # not in config -> shows as auto-discovered/unregistered
}

LAN_IPS = [f"192.168.88.{n}" for n in (10, 12, 14, 21, 33, 50, 77)]
PUBLIC_IPS = [
    "104.21.5.12", "172.217.16.14", "151.101.65.69", "13.107.42.14",
    "185.199.108.153", "1.1.1.1", "8.8.8.8", "142.250.187.14",
]
SCANNER_IPS = [
    "45.148.10.23", "185.220.101.4", "89.248.165.31", "194.26.29.77",
    "196.251.71.9", "23.94.35.180", "91.240.118.22",
]
WAN_IP = "203.0.113.9"

CHAINS_LAN_WEB = [(ip, p) for ip in LAN_IPS for p in (443, 80)]

def line(action, slug, chain, in_if, out_if, proto, src, sport, dst, dport, flags=None, length=None):
    prefix = f"{action}|{slug}|" if action else ""
    conn = "connection-state:new"
    parts = [f"in:{in_if}"]
    if out_if:
        parts[0] += f" out:{out_if}"
    parts.append(conn)
    if proto == "ICMP":
        parts.append(f"proto ICMP ({flags})")
        parts.append(f"{src}->{dst}")
    else:
        pf = f"proto {proto}"
        if flags:
            pf += f" ({flags})"
        parts.append(pf)
        parts.append(f"{src}:{sport}->{dst}:{dport}")
    parts.append(f"len {length or random.randint(52, 1420)}")
    body = ", ".join(parts)
    return f"<134>Jan 15 10:22:31 MikroTik {prefix}{chain}: {body}"

def random_event():
    kind = random.choices(
        ["accept_web", "accept_dns", "accept_mgmt", "drop_invalid", "drop_scan",
         "reject_block", "log_wan", "unknown"],
        weights=[30, 10, 5, 15, 20, 8, 8, 4],
    )[0]

    if kind == "accept_web":
        src, dport = random.choice(CHAINS_LAN_WEB)
        return line("A", "lan-wan", "forward", "bridge-lan", "ether1", "TCP",
                     src, random.randint(40000, 65000), random.choice(PUBLIC_IPS), dport, "SYN")
    if kind == "accept_dns":
        return line("A", "lan-dns", "forward", "bridge-lan", "ether1", "UDP",
                     random.choice(LAN_IPS), random.randint(40000, 65000), "1.1.1.1", 53)
    if kind == "accept_mgmt":
        return line("A", "mgmt-ssh", "input", "bridge-lan", None, "TCP",
                     random.choice(LAN_IPS), random.randint(40000, 65000), "192.168.88.1", 22, "SYN")
    if kind == "drop_invalid":
        return line("D", "invalid", "input", "ether1", None, "TCP",
                     random.choice(SCANNER_IPS), random.randint(1024, 65000), WAN_IP,
                     random.choice([22, 23, 445, 3389, 8291, 5900]), "RST,ACK")
    if kind == "drop_scan":
        return line("D", "input-def", "input", "ether1", None, "TCP",
                     random.choice(SCANNER_IPS), random.randint(1024, 65000), WAN_IP,
                     random.choice([22, 23, 445, 3389, 8080, 8291, 2323, 5900]), "SYN")
    if kind == "reject_block":
        return line("R", "no-torrent", "forward", "bridge-lan", "ether1", "TCP",
                     random.choice(LAN_IPS), random.randint(40000, 65000),
                     random.choice(PUBLIC_IPS), random.choice([6881, 51413]), "SYN")
    if kind == "log_wan":
        return line("L", "wan-test", "input", "ether1", None,
                     random.choice(["TCP", "UDP", "ICMP"]),
                     random.choice(SCANNER_IPS + PUBLIC_IPS), random.randint(1024, 65000),
                     WAN_IP, random.choice([53, 443, 22, 8291]),
                     "SYN" if random.random() > 0.3 else "type 8, code 0")
    return line(None, None, "forward", "bridge-lan", "ether1", "TCP",
                random.choice(LAN_IPS), random.randint(40000, 65000),
                random.choice(PUBLIC_IPS), 443, "SYN")

def main():
    count = int(sys.argv[2]) if len(sys.argv) > 2 else 80
    for _ in range(count):
        device = random.choices(["core", "branch"], weights=[75, 25])[0]
        src_ip = DEVICES[device]
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.bind((src_ip, 0))
        msg = random_event()
        sock.sendto(msg.encode(), TARGET)
        sock.close()
        time.sleep(random.uniform(0.01, 0.05))
    print(f"sent {count} events to {TARGET}")

if __name__ == "__main__":
    main()
