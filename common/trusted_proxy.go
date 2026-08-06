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
	for _, value := range values {
		proxy := strings.TrimSpace(value)
		if proxy == "" {
			continue
		}

		if err := validateTrustedProxy(proxy); err != nil {
			return nil, err
		}
		proxies = append(proxies, proxy)
	}
	return proxies, nil
}

func validateTrustedProxy(proxy string) error {
	if strings.Contains(proxy, "/") {
		_, network, err := net.ParseCIDR(proxy)
		if err != nil {
			return fmt.Errorf("invalid TRUSTED_PROXIES entry %q: %w", proxy, err)
		}
		if ones, _ := network.Mask.Size(); ones == 0 {
			return fmt.Errorf("TRUSTED_PROXIES entry %q trusts every address; configure a specific proxy address or network", proxy)
		}
		return nil
	}

	if net.ParseIP(proxy) == nil {
		return fmt.Errorf("invalid TRUSTED_PROXIES entry %q", proxy)
	}
	return nil
}
