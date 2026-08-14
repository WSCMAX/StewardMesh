package main

// Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.

import "testing"

func TestParseCommandKeepsServerDefaultAndRequiresExactDemoSeedCommand(t *testing.T) {
	server, err := parseCommand([]string{"stewardmesh"})
	if err != nil || server {
		t.Fatalf("unexpected default command mode seed=%t err=%v", server, err)
	}
	seed, err := parseCommand([]string{"stewardmesh", "seed-demo"})
	if err != nil || !seed {
		t.Fatalf("unexpected demo seed command mode seed=%t err=%v", seed, err)
	}
	for _, args := range [][]string{{}, {"stewardmesh", "serve"}, {"stewardmesh", "seed-demo", "extra"}} {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("expected invalid command rejection for %#v", args)
		}
	}
}
