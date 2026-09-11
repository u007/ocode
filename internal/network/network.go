package network

import (
	"net"
	"strings"
	"time"
)

// GetIP returns the machine's preferred LAN IPv4 address for sharing URLs.
// It never returns "" — the fallback is "localhost" only when no usable
// IPv4 address exists at all.
//
// Strategy:
//  1. UDP dial-out trick (validated): the OS-preferred outbound source
//     address, accepted only when it is a usable LAN/global address —
//     tailscale CGNAT, loopback, and link-local results are rejected so a
//     VPN/exit-node route never wins over the reachable LAN interface.
//  2. Interface scan fallback: ranked selection preferring private LAN
//     ranges, skipping down/loopback interfaces and docker/virtual devices
//     for the primary pick (see pickBestIP).
//  3. Last resort: "localhost".
func GetIP() string {
	if ip := outboundIP(); ip != nil && isShareableIP(ip) {
		return ip.String()
	}
	if ip := rankedInterfaceIP(); ip != "" {
		return ip
	}
	return "localhost"
}

// isShareableIP reports whether the outbound-dial result is usable as a LAN
// share address: IPv4, not loopback/link-local, and not tailscale CGNAT
// (100.64/10, tailnet-only).
func isShareableIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
		return false
	}
	if isTailscaleIP(ip4) {
		return false
	}
	return true
}

// outboundIP dials a public address over UDP (no traffic sent) and reports
// the local source address. Returns nil on failure; the caller validates
// the result via isShareableIP.
func outboundIP() net.IP {
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 500*time.Millisecond)
	if err != nil {
		return nil
	}
	defer conn.Close()
	if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return udp.IP
	}
	return nil
}

// rankedInterfaceIP scans interfaces and returns the best candidate as a
// string, or "" when none exists.
func rankedInterfaceIP() string {
	if ip := pickBestIP(collectInterfaceIPs()); ip != nil {
		return ip.String()
	}
	return fallbackAddrsScan()
}

// collectInterfaceIPs gathers IPv4 addresses from up, non-loopback
// interfaces, handling both *net.IPNet and *net.IPAddr forms. Each entry
// records whether its interface looks virtual (docker/veth/bridge) so the
// ranker can deprioritize it without dropping it entirely.
func collectInterfaceIPs() []ifaceIP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []ifaceIP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		virtual := isVirtualInterface(iface.Name)
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch a := addr.(type) {
			case *net.IPNet:
				ip = a.IP
			case *net.IPAddr:
				ip = a.IP
			default:
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				out = append(out, ifaceIP{IP: ip4, Virtual: virtual})
			}
		}
	}
	return out
}

type ifaceIP struct {
	IP      net.IP
	Virtual bool
}

// pickBestIP deterministically selects the best share address:
//  1. non-virtual private LAN (10/8, 172.16/12, 192.168/16)
//  2. non-virtual other global unicast (non-tailscale, non-link-local)
//  3. virtual private LAN (docker bridges etc.)
//  4. virtual other global
//  5. tailscale CGNAT (100.64/10) — tailnet-only, better than localhost
//
// Loopback and link-local addresses never win. Returns nil when empty.
func pickBestIP(ips []ifaceIP) net.IP {
	var (
		lan, global, vLan, vGlobal, ts net.IP
	)
	for _, e := range ips {
		ip := e.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if isTailscaleIP(ip) {
			if ts == nil {
				ts = ip
			}
			continue
		}
		switch {
		case isPrivateLAN(ip) && !e.Virtual:
			if lan == nil {
				lan = ip
			}
		case isPrivateLAN(ip) && e.Virtual:
			if vLan == nil {
				vLan = ip
			}
		case !e.Virtual:
			if global == nil {
				global = ip
			}
		default:
			if vGlobal == nil {
				vGlobal = ip
			}
		}
	}
	for _, c := range []net.IP{lan, global, vLan, vGlobal, ts} {
		if c != nil {
			return c
		}
	}
	return nil
}

// isVirtualInterface reports whether an interface name looks like a
// docker/bridge/virtual device whose address peers on the LAN cannot reach.
func isVirtualInterface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"docker", "veth", "br-", "virbr", "vmnet", "vboxnet", "vmware"} {
		if strings.HasPrefix(n, p) || strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// fallbackAddrsScan is the legacy InterfaceAddrs scan kept as a last resort.
// It feeds the same deterministic ranker so ordering is stable.
func fallbackAddrsScan() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	var ips []ifaceIP
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			ips = append(ips, ifaceIP{IP: ip4})
		}
	}
	if ip := pickBestIP(ips); ip != nil {
		return ip.String()
	}
	return ""
}

func isTailscaleIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] < 128
}

func isPrivateLAN(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	// 10/8
	if ip4[0] == 10 {
		return true
	}
	// 172.16/12
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] < 32 {
		return true
	}
	// 192.168/16
	if ip4[0] == 192 && ip4[1] == 168 {
		return true
	}
	return false
}
