// A curated (not exhaustive) reference of commonly-seen port numbers, for
// the port "investigate" button -- see components/PortInvestigateButton.svelte
// and PortLookupPopover.svelte. Unlike the IP investigate button, this is
// pure local static data: no network call, consistent with SECURITY.md's
// "no outbound calls except the on-demand IP lookup" claim, and ports (unlike
// arbitrary IPs) are a small, well-known, mostly-static space anyway.
//
// Scope is deliberately curated rather than a full IANA registry dump:
// standard/well-known services, common databases, common self-hosted/
// homelab apps (the mikroview audience), remote access, VPN, messaging, and
// a small set of ports historically associated with malware/backdoors that
// are useful context when triaging a firewall log. A port can have more
// than one common meaning (e.g. 53 is both TCP and UDP DNS; 8080 is heavily
// overloaded across unrelated apps) -- lookupPort returns all of them.

export type PortCategory =
  | 'standard'
  | 'database'
  | 'self-hosted'
  | 'remote-access'
  | 'vpn'
  | 'messaging'
  | 'suspicious'

export interface PortInfo {
  protocol: 'tcp' | 'udp' | 'tcp/udp'
  name: string
  description: string
  category: PortCategory
}

const PORTS: Record<number, PortInfo[]> = {
  20: [{ protocol: 'tcp', name: 'FTP-DATA', description: 'FTP data transfer', category: 'standard' }],
  21: [{ protocol: 'tcp', name: 'FTP', description: 'File Transfer Protocol control channel', category: 'standard' }],
  22: [{ protocol: 'tcp', name: 'SSH', description: 'Secure shell remote login', category: 'remote-access' }],
  23: [{ protocol: 'tcp', name: 'Telnet', description: 'Unencrypted remote terminal -- insecure, avoid exposing', category: 'standard' }],
  25: [{ protocol: 'tcp', name: 'SMTP', description: 'Mail transfer between servers', category: 'standard' }],
  53: [{ protocol: 'tcp/udp', name: 'DNS', description: 'Domain name resolution', category: 'standard' }],
  67: [{ protocol: 'udp', name: 'DHCP', description: 'DHCP server', category: 'standard' }],
  68: [{ protocol: 'udp', name: 'DHCP', description: 'DHCP client', category: 'standard' }],
  69: [{ protocol: 'udp', name: 'TFTP', description: 'Trivial file transfer -- often network boot / device firmware', category: 'standard' }],
  80: [{ protocol: 'tcp', name: 'HTTP', description: 'Unencrypted web traffic', category: 'standard' }],
  81: [{ protocol: 'tcp', name: 'HTTP-alt', description: 'Common alt-HTTP; frequently Nginx Proxy Manager’s admin UI', category: 'self-hosted' }],
  88: [{ protocol: 'tcp/udp', name: 'Kerberos', description: 'Network authentication (Active Directory)', category: 'standard' }],
  110: [{ protocol: 'tcp', name: 'POP3', description: 'Mail retrieval (legacy)', category: 'standard' }],
  111: [{ protocol: 'tcp/udp', name: 'RPC', description: 'Unix/Linux portmapper / remote procedure call', category: 'standard' }],
  119: [{ protocol: 'tcp', name: 'NNTP', description: 'Usenet news', category: 'standard' }],
  123: [{ protocol: 'udp', name: 'NTP', description: 'Network time synchronization', category: 'standard' }],
  135: [{ protocol: 'tcp', name: 'MS-RPC', description: 'Windows RPC endpoint mapper', category: 'standard' }],
  137: [{ protocol: 'udp', name: 'NetBIOS-NS', description: 'NetBIOS name service', category: 'standard' }],
  138: [{ protocol: 'udp', name: 'NetBIOS-DGM', description: 'NetBIOS datagram service', category: 'standard' }],
  139: [{ protocol: 'tcp', name: 'NetBIOS-SSN', description: 'NetBIOS session service (legacy SMB)', category: 'standard' }],
  143: [{ protocol: 'tcp', name: 'IMAP', description: 'Mail retrieval, keeps mail on the server', category: 'standard' }],
  161: [{ protocol: 'udp', name: 'SNMP', description: 'Network device monitoring/management', category: 'standard' }],
  162: [{ protocol: 'udp', name: 'SNMP-Trap', description: 'SNMP event notifications', category: 'standard' }],
  179: [{ protocol: 'tcp', name: 'BGP', description: 'Border Gateway Protocol routing', category: 'standard' }],
  194: [{ protocol: 'tcp', name: 'IRC', description: 'Internet Relay Chat', category: 'standard' }],
  389: [{ protocol: 'tcp/udp', name: 'LDAP', description: 'Directory service (Active Directory, etc.)', category: 'standard' }],
  443: [{ protocol: 'tcp', name: 'HTTPS', description: 'Encrypted web traffic (TLS)', category: 'standard' }],
  445: [{ protocol: 'tcp', name: 'SMB', description: 'Windows file sharing / Active Directory', category: 'standard' }],
  465: [{ protocol: 'tcp', name: 'SMTPS', description: 'SMTP submission with implicit TLS', category: 'standard' }],
  500: [{ protocol: 'udp', name: 'IKE', description: 'IPsec VPN key exchange', category: 'vpn' }],
  514: [{ protocol: 'udp', name: 'Syslog', description: 'System logging -- this is mikroview’s own listener', category: 'standard' }],
  515: [{ protocol: 'tcp', name: 'LPD', description: 'Line printer daemon', category: 'standard' }],
  520: [{ protocol: 'udp', name: 'RIP', description: 'Routing Information Protocol', category: 'standard' }],
  587: [{ protocol: 'tcp', name: 'SMTP-Submission', description: 'Mail client submission (STARTTLS)', category: 'standard' }],
  631: [{ protocol: 'tcp/udp', name: 'IPP', description: 'Internet Printing Protocol / CUPS', category: 'standard' }],
  636: [{ protocol: 'tcp', name: 'LDAPS', description: 'LDAP over TLS', category: 'standard' }],
  993: [{ protocol: 'tcp', name: 'IMAPS', description: 'IMAP over TLS', category: 'standard' }],
  995: [{ protocol: 'tcp', name: 'POP3S', description: 'POP3 over TLS', category: 'standard' }],

  1080: [{ protocol: 'tcp', name: 'SOCKS', description: 'SOCKS proxy', category: 'standard' }],
  1194: [{ protocol: 'udp', name: 'OpenVPN', description: 'OpenVPN tunnel (default)', category: 'vpn' }],
  1433: [{ protocol: 'tcp', name: 'MSSQL', description: 'Microsoft SQL Server', category: 'database' }],
  1521: [{ protocol: 'tcp', name: 'Oracle', description: 'Oracle database listener', category: 'database' }],
  1701: [{ protocol: 'udp', name: 'L2TP', description: 'Layer 2 Tunneling Protocol VPN', category: 'vpn' }],
  1723: [{ protocol: 'tcp', name: 'PPTP', description: 'Point-to-Point Tunneling Protocol VPN -- legacy, cryptographically weak', category: 'vpn' }],
  1883: [{ protocol: 'tcp', name: 'MQTT', description: 'IoT publish/subscribe messaging', category: 'messaging' }],
  1900: [{ protocol: 'udp', name: 'SSDP', description: 'UPnP device discovery', category: 'standard' }],
  2049: [{ protocol: 'tcp/udp', name: 'NFS', description: 'Network File System', category: 'standard' }],
  2181: [{ protocol: 'tcp', name: 'ZooKeeper', description: 'Apache ZooKeeper coordination service', category: 'database' }],
  2222: [{ protocol: 'tcp', name: 'SSH-alt', description: 'Common alternate SSH port', category: 'remote-access' }],
  2283: [{ protocol: 'tcp', name: 'Immich', description: 'Immich self-hosted photo/video management', category: 'self-hosted' }],
  2375: [{ protocol: 'tcp', name: 'Docker', description: 'Docker daemon API, unencrypted -- avoid exposing this', category: 'self-hosted' }],
  2376: [{ protocol: 'tcp', name: 'Docker-TLS', description: 'Docker daemon API over TLS', category: 'self-hosted' }],
  3000: [{ protocol: 'tcp', name: 'HTTP-dev/Grafana', description: 'Grafana dashboards; also a very common generic dev-server port', category: 'self-hosted' }],
  3001: [{ protocol: 'tcp', name: 'Uptime Kuma', description: 'Uptime Kuma self-hosted monitoring', category: 'self-hosted' }],
  3128: [{ protocol: 'tcp', name: 'Squid', description: 'Squid caching web proxy', category: 'standard' }],
  3306: [{ protocol: 'tcp', name: 'MySQL', description: 'MySQL / MariaDB database', category: 'database' }],
  3389: [{ protocol: 'tcp', name: 'RDP', description: 'Windows Remote Desktop', category: 'remote-access' }],
  5000: [{ protocol: 'tcp', name: 'HTTP-alt', description: 'Heavily overloaded: Flask/dev servers, Synology DSM, UPnP control, macOS AirPlay', category: 'standard' }],
  5001: [{ protocol: 'tcp', name: 'Synology-HTTPS', description: 'Synology DSM HTTPS admin UI', category: 'self-hosted' }],
  5060: [{ protocol: 'tcp/udp', name: 'SIP', description: 'VoIP call signaling', category: 'standard' }],
  5432: [{ protocol: 'tcp', name: 'PostgreSQL', description: 'PostgreSQL database', category: 'database' }],
  5601: [{ protocol: 'tcp', name: 'Kibana', description: 'Elasticsearch log visualization', category: 'self-hosted' }],
  5672: [{ protocol: 'tcp', name: 'AMQP', description: 'RabbitMQ message broker', category: 'messaging' }],
  5900: [{ protocol: 'tcp', name: 'VNC', description: 'Remote desktop (VNC)', category: 'remote-access' }],
  5985: [{ protocol: 'tcp', name: 'WinRM', description: 'Windows Remote Management (HTTP)', category: 'remote-access' }],
  5986: [{ protocol: 'tcp', name: 'WinRM-HTTPS', description: 'Windows Remote Management (HTTPS)', category: 'remote-access' }],
  6379: [{ protocol: 'tcp', name: 'Redis', description: 'In-memory data store/cache -- often mistakenly exposed with no auth', category: 'database' }],
  6667: [{ protocol: 'tcp', name: 'IRC', description: 'Internet Relay Chat (alt port)', category: 'standard' }],
  7878: [{ protocol: 'tcp', name: 'Radarr', description: 'Radarr movie collection management', category: 'self-hosted' }],
  8006: [{ protocol: 'tcp', name: 'Proxmox', description: 'Proxmox VE web UI', category: 'self-hosted' }],
  8080: [{ protocol: 'tcp', name: 'HTTP-alt', description: 'Extremely overloaded alt-HTTP port -- proxies, dev servers, many self-hosted apps', category: 'standard' }],
  8096: [{ protocol: 'tcp', name: 'Jellyfin', description: 'Jellyfin media server', category: 'self-hosted' }],
  8123: [{ protocol: 'tcp', name: 'Home Assistant', description: 'Home Assistant web UI', category: 'self-hosted' }],
  8291: [{ protocol: 'tcp', name: 'Winbox', description: 'MikroTik RouterOS Winbox management', category: 'self-hosted' }],
  8443: [{ protocol: 'tcp', name: 'HTTPS-alt', description: 'Alternate HTTPS, often admin UIs', category: 'standard' }],
  8728: [{ protocol: 'tcp', name: 'MikroTik API', description: 'MikroTik RouterOS API', category: 'self-hosted' }],
  8729: [{ protocol: 'tcp', name: 'MikroTik API-SSL', description: 'MikroTik RouterOS API over TLS', category: 'self-hosted' }],
  8883: [{ protocol: 'tcp', name: 'MQTT-TLS', description: 'MQTT over TLS', category: 'messaging' }],
  8989: [{ protocol: 'tcp', name: 'Sonarr', description: 'Sonarr TV show management', category: 'self-hosted' }],
  9000: [{ protocol: 'tcp', name: 'Portainer', description: 'Portainer Docker management UI (HTTP)', category: 'self-hosted' }],
  9090: [{ protocol: 'tcp', name: 'Prometheus', description: 'Prometheus metrics/monitoring', category: 'self-hosted' }],
  9092: [{ protocol: 'tcp', name: 'Kafka', description: 'Apache Kafka broker', category: 'messaging' }],
  9200: [{ protocol: 'tcp', name: 'Elasticsearch', description: 'Elasticsearch HTTP API', category: 'database' }],
  9443: [{ protocol: 'tcp', name: 'Portainer-HTTPS', description: 'Portainer Docker management UI (HTTPS)', category: 'self-hosted' }],
  9696: [{ protocol: 'tcp', name: 'Prowlarr', description: 'Prowlarr indexer management', category: 'self-hosted' }],
  11211: [{ protocol: 'tcp/udp', name: 'Memcached', description: 'In-memory caching -- often exposed unauthenticated by mistake', category: 'database' }],
  15672: [{ protocol: 'tcp', name: 'RabbitMQ Admin', description: 'RabbitMQ management UI', category: 'self-hosted' }],
  27017: [{ protocol: 'tcp', name: 'MongoDB', description: 'MongoDB database', category: 'database' }],
  32400: [{ protocol: 'tcp', name: 'Plex', description: 'Plex Media Server', category: 'self-hosted' }],
  51820: [{ protocol: 'udp', name: 'WireGuard', description: 'WireGuard VPN (default)', category: 'vpn' }],

  4444: [{ protocol: 'tcp', name: 'Metasploit', description: 'Common default handler port for Metasploit/reverse shells; occasionally legitimate too', category: 'suspicious' }],
  6666: [{ protocol: 'tcp', name: 'IRC-bot', description: 'Historically associated with IRC-based botnet command-and-control', category: 'suspicious' }],
  12345: [{ protocol: 'tcp', name: 'NetBus', description: 'Legacy NetBus backdoor trojan default port', category: 'suspicious' }],
  31337: [{ protocol: 'tcp', name: 'Back Orifice', description: '"Elite" port -- classic Back Orifice backdoor default; mostly opportunistic-scan noise today', category: 'suspicious' }],
}

export function lookupPort(port: number): PortInfo[] | undefined {
  return PORTS[port]
}
