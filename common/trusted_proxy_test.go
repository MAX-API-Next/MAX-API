package common

import (
	"slices"
	"testing"
)

func TestDefaultTrustedProxiesOnlyTrustLoopback(t *testing.T) {
	expected := []string{"127.0.0.0/8", "::1/128"}
	if !slices.Equal(DefaultTrustedProxies, expected) {
		t.Fatalf("default trusted proxies must only include loopback networks: %v", DefaultTrustedProxies)
	}
}

func TestParseTrustedProxiesUsesLoopbackDefaults(t *testing.T) {
	proxies, err := parseTrustedProxies("")
	if err != nil {
		t.Fatalf("parseTrustedProxies returned error: %v", err)
	}
	if !slices.Equal(proxies, DefaultTrustedProxies) {
		t.Fatalf("unexpected default trusted proxies: %v", proxies)
	}
}

func TestParseTrustedProxiesRejectsUnrestrictedNetworks(t *testing.T) {
	for _, value := range []string{"0.0.0.0/0", "::/0", "not-a-network"} {
		if _, err := parseTrustedProxies(value); err == nil {
			t.Fatalf("expected unrestricted proxy network %q to be rejected", value)
		}
	}
}

func TestParseTrustedProxiesAcceptsExplicitProxyNetworks(t *testing.T) {
	proxies, err := parseTrustedProxies("127.0.0.1, 10.0.0.0/8")
	if err != nil {
		t.Fatalf("parseTrustedProxies rejected explicit proxy networks: %v", err)
	}
	if !slices.Equal(proxies, []string{"127.0.0.1", "10.0.0.0/8"}) {
		t.Fatalf("unexpected explicit trusted proxies: %v", proxies)
	}
}
