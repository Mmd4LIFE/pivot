package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunPrintsVersion(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		nil,
		{"version"},
		{"--version"},
		{"-v"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			if err := run(args, &out); err != nil {
				t.Fatalf("run(%v) returned error: %v", args, err)
			}

			if got := out.String(); !strings.HasPrefix(got, "pivot ") {
				t.Errorf("run(%v) = %q, want it to start with %q", args, got, "pivot ")
			}
		})
	}
}

func TestRunPrintsUsage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			if err := run(args, &out); err != nil {
				t.Fatalf("run(%v) returned error: %v", args, err)
			}

			if got := out.String(); !strings.Contains(got, "Usage:") {
				t.Errorf("run(%v) = %q, want it to contain %q", args, got, "Usage:")
			}
		})
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := run([]string{"serve"}, &out)

	if err == nil {
		t.Fatal("run([serve]) returned nil error; want an unknown-command error")
	}

	// The error must name the offending command so the message is actionable.
	if !strings.Contains(err.Error(), "serve") {
		t.Errorf("error = %q, want it to name the unknown command", err)
	}
}
