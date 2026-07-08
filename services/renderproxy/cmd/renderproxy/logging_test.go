package main

import "testing"

func TestConfigureLogging(t *testing.T) {
	if err := configureLogging(envFrom(map[string]string{"LOG_LEVEL": "debug"})); err != nil {
		t.Fatalf("valid level: %v", err)
	}
	if err := configureLogging(envFrom(map[string]string{})); err != nil {
		t.Fatalf("default level: %v", err)
	}
	if err := configureLogging(envFrom(map[string]string{"LOG_LEVEL": "nonsense"})); err == nil {
		t.Fatal("invalid level should error")
	}
}
