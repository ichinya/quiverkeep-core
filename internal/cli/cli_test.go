package cli

import (
	"context"
	"testing"
	"time"
)

func TestRootCommandHasExpectedSubcommands(t *testing.T) {
	t.Parallel()

	root := buildRootCommand(context.Background(), &Options{})
	expected := map[string]bool{
		"serve":   false,
		"status":  false,
		"usage":   false,
		"limits":  false,
		"config":  false,
		"doctor":  false,
		"version": false,
	}

	for _, command := range root.Commands() {
		if _, ok := expected[command.Name()]; ok {
			expected[command.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("expected subcommand %s", name)
		}
	}
}

func TestRootCommandBuildPerformance(t *testing.T) {
	t.Parallel()

	started := time.Now()
	_ = buildRootCommand(context.Background(), &Options{})
	duration := time.Since(started)
	if duration > 50*time.Millisecond {
		t.Fatalf("expected command bootstrap <50ms, got %s", duration)
	}
}
