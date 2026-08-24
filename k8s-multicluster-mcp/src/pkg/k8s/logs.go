package k8s

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	DefaultLogSinceSeconds = 60 * 60
	MaxLogSinceSeconds     = 7 * 24 * 60 * 60
	DefaultPodLogTailLines = 100
	DefaultDebugLogLines   = 50
	MaxLogTailLines        = 1000
)

func validateLogQuery(tailLines, sinceSeconds int) error {
	if tailLines < 1 || tailLines > MaxLogTailLines {
		return fmt.Errorf("tail lines must be between 1 and %d", MaxLogTailLines)
	}
	if sinceSeconds < 1 || sinceSeconds > MaxLogSinceSeconds {
		return fmt.Errorf("log window must be between 1 and %d seconds", MaxLogSinceSeconds)
	}
	return nil
}

func newPodLogOptions(container string, tailLines, sinceSeconds int) *corev1.PodLogOptions {
	tailLinesValue := int64(tailLines)
	sinceSecondsValue := int64(sinceSeconds)
	return &corev1.PodLogOptions{
		Container:    container,
		TailLines:    &tailLinesValue,
		SinceSeconds: &sinceSecondsValue,
		Timestamps:   true,
	}
}

func logWindowMetadata(observedAt time.Time, tailLines, sinceSeconds int) map[string]interface{} {
	observedAt = observedAt.UTC()
	return map[string]interface{}{
		"observedAt":            observedAt,
		"windowStart":           observedAt.Add(-time.Duration(sinceSeconds) * time.Second),
		"sinceSeconds":          sinceSeconds,
		"tailLinesPerContainer": tailLines,
		"timestamps":            true,
	}
}
