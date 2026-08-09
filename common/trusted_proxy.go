package common

import (
	"fmt"
	"net"
	"strings"
)

func parseTrustedProxies(raw string) ([]string, error) {
	values := DefaultTrustedProxies
	if strings.TrimSpace(raw) != "" {
		values = strings.Split(raw, ",")
	}

	proxies := make([]string, 0, len(values))
	prefixes := make([]trustedProxyPrefix, 0, len(values))
	for _, value := range values {
		proxy := strings.TrimSpace(value)
		if proxy == "" {
			continue
		}

		prefix, err := parseTrustedProxyPrefix(proxy)
		if err != nil {
			return nil, err
		}
		if prefix.prefix == 0 {
			return nil, unrestrictedTrustedProxyError(proxy, prefix.bits)
		}

		prefixes = append(prefixes, prefix)
		proxies = append(proxies, proxy)
	}

	for _, bits := range []int{32, 128} {
		if trustedProxyPrefixesCoverAll(prefixes, bits) {
			return nil, fmt.Errorf("TRUSTED_PROXIES entries trust every %s address; configure a specific proxy address or network", trustedProxyAddressFamily(bits))
		}
	}

	return proxies, nil
}

type trustedProxyPrefix struct {
	ip     net.IP
	bits   int
	prefix int
}

func parseTrustedProxyPrefix(proxy string) (trustedProxyPrefix, error) {
	if strings.Contains(proxy, "/") {
		_, network, err := net.ParseCIDR(proxy)
		if err != nil {
			return trustedProxyPrefix{}, fmt.Errorf("invalid TRUSTED_PROXIES entry %q: %w", proxy, err)
		}

		ip := network.IP
		mask := network.Mask
		if ipv4 := ip.To4(); ipv4 != nil {
			if len(mask) == net.IPv6len {
				mask = mask[net.IPv6len-net.IPv4len:]
			}
			ones, bits := mask.Size()
			if bits != net.IPv4len*8 {
				return trustedProxyPrefix{}, fmt.Errorf("invalid TRUSTED_PROXIES entry %q", proxy)
			}
			return trustedProxyPrefix{ip: ipv4, bits: bits, prefix: ones}, nil
		}

		ones, bits := mask.Size()
		if bits != net.IPv6len*8 {
			return trustedProxyPrefix{}, fmt.Errorf("invalid TRUSTED_PROXIES entry %q", proxy)
		}
		return trustedProxyPrefix{ip: ip, bits: bits, prefix: ones}, nil
	}

	ip := net.ParseIP(proxy)
	if ip == nil {
		return trustedProxyPrefix{}, fmt.Errorf("invalid TRUSTED_PROXIES entry %q", proxy)
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return trustedProxyPrefix{ip: ipv4, bits: net.IPv4len * 8, prefix: net.IPv4len * 8}, nil
	}
	return trustedProxyPrefix{ip: ip, bits: net.IPv6len * 8, prefix: net.IPv6len * 8}, nil
}

func unrestrictedTrustedProxyError(proxy string, bits int) error {
	return fmt.Errorf("TRUSTED_PROXIES entry %q trusts every %s address; configure a specific proxy address or network", proxy, trustedProxyAddressFamily(bits))
}

func trustedProxyAddressFamily(bits int) string {
	if bits == net.IPv4len*8 {
		return "IPv4"
	}
	return "IPv6"
}

type trustedProxyCoverageNode struct {
	covered  bool
	children [2]*trustedProxyCoverageNode
}

func trustedProxyPrefixesCoverAll(prefixes []trustedProxyPrefix, bits int) bool {
	root := &trustedProxyCoverageNode{}
	for _, prefix := range prefixes {
		if prefix.bits != bits {
			continue
		}
		addTrustedProxyPrefix(root, prefix)
	}
	return trustedProxyCoverageComplete(root)
}

func addTrustedProxyPrefix(root *trustedProxyCoverageNode, prefix trustedProxyPrefix) {
	if root.covered {
		return
	}
	if prefix.prefix == 0 {
		root.covered = true
		return
	}

	node := root
	for bitIndex := 0; bitIndex < prefix.prefix; bitIndex++ {
		byteIndex := bitIndex / 8
		bitOffset := 7 - (bitIndex % 8)
		branch := (prefix.ip[byteIndex] >> bitOffset) & 1
		if node.children[branch] == nil {
			node.children[branch] = &trustedProxyCoverageNode{}
		}
		node = node.children[branch]
		if node.covered {
			return
		}
	}
	node.covered = true
}

func trustedProxyCoverageComplete(node *trustedProxyCoverageNode) bool {
	if node == nil {
		return false
	}
	if node.covered {
		return true
	}
	return trustedProxyCoverageComplete(node.children[0]) && trustedProxyCoverageComplete(node.children[1])
}
