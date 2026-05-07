package main

import (
	"testing"
	"time"
)

func TestProviderOpTimeout_ExtendsInitForManagedStartup(t *testing.T) {
	if got := providerOpTimeout("init"); got != 120*time.Second {
		t.Fatalf("providerOpTimeout(init) = %s, want %s", got, 120*time.Second)
	}
	if got := providerOpTimeout("health"); got != 30*time.Second {
		t.Fatalf("providerOpTimeout(health) = %s, want %s", got, 30*time.Second)
	}
}
