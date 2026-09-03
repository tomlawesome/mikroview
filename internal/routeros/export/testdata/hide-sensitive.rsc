# 2026/09/01 10:00:00 by RouterOS 7.24.1
# software id = ABCD-1234
#
# model = CHR
# serial number =
/interface bridge
add name=bridge1

/interface list
add name=WAN
add name=LAN

/ip pool
add name=dhcp_pool0 ranges=192.168.88.10-192.168.88.254

/ip address
add address=192.168.88.1/24 interface=bridge1 network=192.168.88.0
add address=203.0.113.5/29 interface=ether1 network=203.0.113.0

/ip firewall filter
add action=accept chain=input comment="allow established" connection-state=established,related
add action=accept chain=forward comment="lan to wan" connection-state=established,related \
    in-interface=bridge1 out-interface=ether1 log=no log-prefix=""
add action=drop chain=forward comment="block wan to lan, unsolicited" in-interface=ether1 \
    out-interface=bridge1
add action=accept chain=forward comment="guest network to wan only" in-interface-list=GUEST \
    out-interface=ether1 log=yes log-prefix="A|guest|"
add action=drop chain=forward comment="drop everything else leaving bridge1" \
    in-interface=bridge1
add action=reject chain=forward comment="reject intra-lan pair (a \"noisy\" host)" in-interface=bridge1 \
    out-interface=bridge1 log=yes log-prefix="R|custom|" reject-with=icmp-network-unreachable
add action=drop chain=forward disabled=yes comment="disabled test rule -- do not enable"

/ip firewall nat
add action=masquerade chain=srcnat out-interface=ether1

/system identity
set name=edge-1
