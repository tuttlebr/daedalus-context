package k8s

import (
	"testing"
	"time"
)

func TestNewPodLogOptionsBoundsByTimeAndLines(t *testing.T) {
	options := newPodLogOptions("sidecar", 125, 900)

	if options.Container != "sidecar" {
		t.Fatalf("container = %q, want sidecar", options.Container)
	}
	if options.TailLines == nil || *options.TailLines != 125 {
		t.Fatalf("tail lines = %v, want 125", options.TailLines)
	}
	if options.SinceSeconds == nil || *options.SinceSeconds != 900 {
		t.Fatalf("since seconds = %v, want 900", options.SinceSeconds)
	}
	if !options.Timestamps {
		t.Fatal("Kubernetes timestamps must be enabled")
	}
}

func TestValidateLogQuery(t *testing.T) {
	for _, test := range []struct {
		name         string
		tailLines    int
		sinceSeconds int
		wantError    bool
	}{
		{name: "valid", tailLines: 100, sinceSeconds: 3600},
		{name: "zero lines", tailLines: 0, sinceSeconds: 3600, wantError: true},
		{name: "too many lines", tailLines: MaxLogTailLines + 1, sinceSeconds: 3600, wantError: true},
		{name: "zero window", tailLines: 100, sinceSeconds: 0, wantError: true},
		{name: "window too large", tailLines: 100, sinceSeconds: MaxLogSinceSeconds + 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateLogQuery(test.tailLines, test.sinceSeconds)
			if (err != nil) != test.wantError {
				t.Fatalf("validateLogQuery() error = %v, wantError %t", err, test.wantError)
			}
		})
	}
}

func TestLogWindowMetadata(t *testing.T) {
	observedAt := time.Date(2026, time.August, 18, 16, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	metadata := logWindowMetadata(observedAt, 50, 3600)

	if got, ok := metadata["observedAt"].(time.Time); !ok || !got.Equal(observedAt) || got.Location() != time.UTC {
		t.Fatalf("observedAt = %v, want UTC %v", got, observedAt.UTC())
	}
	if got, ok := metadata["windowStart"].(time.Time); !ok || !got.Equal(observedAt.Add(-time.Hour)) {
		t.Fatalf("windowStart = %v, want %v", got, observedAt.Add(-time.Hour))
	}
	if metadata["timestamps"] != true {
		t.Fatal("metadata must state that timestamps are enabled")
	}
}
