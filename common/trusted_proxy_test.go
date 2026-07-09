package common

import (
	"slices"
	"testing"
)

func TestDefaultTrustedProxiesOnlyTrustLoopback(t *testing.T) {
	for _, proxy := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "fc00::/7", "fe80::/10"} {
		if slices.Contains(DefaultTrustedProxies, proxy) {
			t.Fatalf("default trusted proxies must not include broad private range %s", proxy)
		}
	}
}
