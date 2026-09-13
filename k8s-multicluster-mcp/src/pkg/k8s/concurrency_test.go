package k8s

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunBoundedTasksHonorsLimit(t *testing.T) {
	const (
		limit     = 3
		taskCount = 12
	)

	var active atomic.Int32
	var maximum atomic.Int32
	var started atomic.Int32
	limitReached := make(chan struct{})
	release := make(chan struct{})
	var signalOnce sync.Once
	tasks := make([]func(), taskCount)
	for i := range tasks {
		tasks[i] = func() {
			current := active.Add(1)
			for {
				previous := maximum.Load()
				if current <= previous || maximum.CompareAndSwap(previous, current) {
					break
				}
			}
			if started.Add(1) == limit {
				signalOnce.Do(func() { close(limitReached) })
			}
			<-release
			active.Add(-1)
		}
	}

	done := make(chan struct{})
	go func() {
		runBoundedTasks(limit, tasks)
		close(done)
	}()

	select {
	case <-limitReached:
		if got := active.Load(); got != limit {
			close(release)
			t.Fatalf("active tasks = %d, want %d", got, limit)
		}
		close(release)
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("bounded tasks did not run concurrently")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bounded tasks did not complete")
	}
	if got := maximum.Load(); got > limit {
		t.Fatalf("maximum concurrency = %d, want at most %d", got, limit)
	}
	if got := started.Load(); got != taskCount {
		t.Fatalf("started tasks = %d, want %d", got, taskCount)
	}
}

func TestGetTopNodesUsesConcurrentSnapshotsAndOnePodList(t *testing.T) {
	var started atomic.Int32
	var podLists atomic.Int32
	readsStarted := make(chan struct{})
	release := make(chan struct{})
	var signalOnce sync.Once

	client := newClusterSummaryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/nodes", "/apis/metrics.k8s.io/v1beta1/nodes", "/api/v1/pods":
			if r.URL.Path == "/api/v1/pods" {
				podLists.Add(1)
			}
			if started.Add(1) == 3 {
				signalOnce.Do(func() { close(readsStarted) })
			}
			select {
			case <-release:
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/nodes":
					_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"NodeList","items":[{"metadata":{"name":"node-a"},"status":{"capacity":{"cpu":"4","memory":"8Gi","pods":"10"}}}]}`)
				case "/apis/metrics.k8s.io/v1beta1/nodes":
					_, _ = fmt.Fprint(w, `{"apiVersion":"metrics.k8s.io/v1beta1","kind":"NodeMetricsList","items":[{"metadata":{"name":"node-a"},"timestamp":"2026-09-12T00:00:00Z","window":"1m","usage":{"cpu":"1","memory":"1Gi"}}]}`)
				case "/api/v1/pods":
					_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"PodList","items":[{"metadata":{"name":"pod-a","namespace":"default"},"spec":{"nodeName":"node-a","containers":[{"name":"app"}]}}]}`)
				}
			case <-r.Context().Done():
			}
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type topNodesResult struct {
		nodes []map[string]interface{}
		err   error
	}
	done := make(chan topNodesResult, 1)
	go func() {
		nodes, err := client.GetTopNodes(ctx, "cpu", false)
		done <- topNodesResult{nodes: nodes, err: err}
	}()

	select {
	case <-readsStarted:
		close(release)
	case <-ctx.Done():
		close(release)
		t.Fatal("node, metrics, and pod snapshots did not overlap")
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("GetTopNodes returned an error: %v", got.err)
	}
	if got := podLists.Load(); got != 1 {
		t.Fatalf("cluster-wide pod lists = %d, want 1", got)
	}
	if len(got.nodes) != 1 || got.nodes[0]["podCount"] != 1 {
		t.Fatalf("pod count was not derived from the shared snapshot: %#v", got.nodes)
	}
}

func TestGetClusterHealthStartsIndependentReadsConcurrently(t *testing.T) {
	var started atomic.Int32
	var metricsLists atomic.Int32
	readsStarted := make(chan struct{})
	release := make(chan struct{})
	var signalOnce sync.Once

	client := newClusterSummaryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apis/metrics.k8s.io/v1beta1/nodes" {
			metricsLists.Add(1)
		}
		if started.Add(1) == 8 {
			signalOnce.Do(func() { close(readsStarted) })
		}
		select {
		case <-release:
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/nodes":
				_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"NodeList","items":[{"metadata":{"name":"node-a"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`)
			case "/apis/metrics.k8s.io/v1beta1/nodes":
				_, _ = fmt.Fprint(w, `{"apiVersion":"metrics.k8s.io/v1beta1","kind":"NodeMetricsList","items":[{"metadata":{"name":"node-a"},"timestamp":"2026-09-12T00:00:00Z","window":"1m","usage":{"cpu":"1","memory":"1Gi"}}]}`)
			default:
				writeClusterSummaryTestResponse(w, r.URL.Path, "")
			}
		case <-r.Context().Done():
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type clusterHealthResult struct {
		health map[string]interface{}
		err    error
	}
	done := make(chan clusterHealthResult, 1)
	go func() {
		health, err := client.GetClusterHealth(ctx, true, true)
		done <- clusterHealthResult{health: health, err: err}
	}()

	select {
	case <-readsStarted:
		close(release)
	case <-ctx.Done():
		close(release)
		t.Fatal("cluster health reads did not overlap")
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("GetClusterHealth returned an error: %v", got.err)
	}
	if got := metricsLists.Load(); got != 1 {
		t.Fatalf("node metrics lists = %d, want 1", got)
	}
	nodeSummary, ok := got.health["nodes"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing node summary: %#v", got.health)
	}
	nodeDetails, ok := nodeSummary["nodes"].([]map[string]interface{})
	if !ok || len(nodeDetails) != 1 || nodeDetails[0]["metrics"] == nil {
		t.Fatalf("node metrics were not joined from the shared list: %#v", nodeSummary)
	}
}

func TestGetPodsLogsReadsContainersConcurrentlyAndPreservesOrder(t *testing.T) {
	var started atomic.Int32
	logsStarted := make(chan struct{})
	release := make(chan struct{})
	var signalOnce sync.Once

	client := newClusterSummaryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/default/pods/demo":
			_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"demo","namespace":"default"},"spec":{"containers":[{"name":"first"},{"name":"second"}]}}`)
		case "/api/v1/namespaces/default/pods/demo/log":
			if started.Add(1) == 2 {
				signalOnce.Do(func() { close(logsStarted) })
			}
			select {
			case <-release:
				_, _ = fmt.Fprint(w, r.URL.Query().Get("container")+" logs")
			case <-r.Context().Done():
			}
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type logResult struct {
		logs string
		err  error
	}
	done := make(chan logResult, 1)
	go func() {
		logs, err := client.GetPodsLogs(ctx, "default", "", "demo", 100, 60)
		done <- logResult{logs: logs, err: err}
	}()

	select {
	case <-logsStarted:
		close(release)
	case <-ctx.Done():
		close(release)
		t.Fatal("container log reads did not overlap")
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("GetPodsLogs returned an error: %v", got.err)
	}
	first := strings.Index(got.logs, "--- Logs for container first ---")
	second := strings.Index(got.logs, "--- Logs for container second ---")
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("container logs are not in pod order: %q", got.logs)
	}
	if !strings.Contains(got.logs, "first logs") || !strings.Contains(got.logs, "second logs") {
		t.Fatalf("container log content missing: %q", got.logs)
	}
}
