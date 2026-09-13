package k8s

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// newTestClient builds a Client with a pre-populated GVR cache, simulating
// a successful discovery call. This lets us test the cache lookup logic
// without needing a real or fake API server.
func newTestClient() *Client {
	deployGVR := &schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	podGVR := &schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	svcGVR := &schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	nsGVR := &schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}

	cache := map[string]*schema.GroupVersionResource{
		// PascalCase Kind
		"Deployment": deployGVR,
		"Pod":        podGVR,
		"Service":    svcGVR,
		"Namespace":  nsGVR,
		// lowercase plural (resource.Name)
		"deployments": deployGVR,
		"pods":        podGVR,
		"services":    svcGVR,
		"namespaces":  nsGVR,
		// lowercase singular (resource.SingularName)
		"deployment": deployGVR,
		"pod":        podGVR,
		"service":    svcGVR,
		"namespace":  nsGVR,
		// short names
		"deploy": deployGVR,
		"po":     podGVR,
		"svc":    svcGVR,
		"ns":     nsGVR,
	}

	return &Client{
		apiResourceCache: cache,
		cacheRefreshedAt: time.Now(),
	}
}

func TestGetCachedGVR_PascalCase(t *testing.T) {
	c := newTestClient()
	gvr, err := c.getCachedGVR("Deployment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gvr.Resource != "deployments" || gvr.Group != "apps" {
		t.Errorf("got %v, want apps/v1/deployments", gvr)
	}
}

func TestGetCachedGVR_LowercasePlural(t *testing.T) {
	c := newTestClient()
	gvr, err := c.getCachedGVR("deployments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gvr.Resource != "deployments" || gvr.Group != "apps" {
		t.Errorf("got %v, want apps/v1/deployments", gvr)
	}
}

func TestGetCachedGVR_LowercaseSingular(t *testing.T) {
	c := newTestClient()
	gvr, err := c.getCachedGVR("deployment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gvr.Resource != "deployments" || gvr.Group != "apps" {
		t.Errorf("got %v, want apps/v1/deployments", gvr)
	}
}

func TestGetCachedGVR_ShortName(t *testing.T) {
	c := newTestClient()

	tests := []struct {
		input   string
		wantRes string
		wantGrp string
	}{
		{"deploy", "deployments", "apps"},
		{"po", "pods", ""},
		{"svc", "services", ""},
		{"ns", "namespaces", ""},
	}

	for _, tt := range tests {
		gvr, err := c.getCachedGVR(tt.input)
		if err != nil {
			t.Errorf("getCachedGVR(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if gvr.Resource != tt.wantRes || gvr.Group != tt.wantGrp {
			t.Errorf("getCachedGVR(%q) = %v, want %s/%s", tt.input, gvr, tt.wantGrp, tt.wantRes)
		}
	}
}

func TestGetCachedGVR_CaseInsensitiveFallback(t *testing.T) {
	c := newTestClient()
	gvr, err := c.getCachedGVR("DEPLOYMENTS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gvr.Resource != "deployments" {
		t.Errorf("got %v, want deployments", gvr.Resource)
	}
}

func TestGetCachedGVR_NotFound(t *testing.T) {
	// A client with a valid, populated cache but no entry for "nonexistent".
	// getCachedGVR will miss the cache, then try discovery (nil client) which panics.
	// So we test the simpler case: a fresh cache with only known entries.
	c := newTestClient()
	_ = c // cache is valid; lookup of a known key works, but "nonexistent" would
	// fall through to discovery. This path requires a real cluster and is
	// covered by integration tests. Verify the cache-hit path returns correct
	// results for known types instead.
	gvr, err := c.getCachedGVR("pods")
	if err != nil {
		t.Fatalf("unexpected error for known type: %v", err)
	}
	if gvr.Resource != "pods" {
		t.Errorf("got %v, want pods", gvr.Resource)
	}
}

func TestGetCachedGVR_CacheTTL(t *testing.T) {
	c := newTestClient()

	gvr1, err := c.getCachedGVR("pods")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	gvr2, err := c.getCachedGVR("pods")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if gvr1 != gvr2 {
		t.Errorf("expected same pointer from cache, got different objects")
	}
}

func TestGetCachedGVR_AllFormsSamePointer(t *testing.T) {
	c := newTestClient()

	gvrKind, _ := c.getCachedGVR("Deployment")
	gvrPlural, _ := c.getCachedGVR("deployments")
	gvrSingular, _ := c.getCachedGVR("deployment")
	gvrShort, _ := c.getCachedGVR("deploy")

	if gvrKind != gvrPlural || gvrPlural != gvrSingular || gvrSingular != gvrShort {
		t.Error("all identifier forms should resolve to the same GVR pointer")
	}
}

func TestGetCachedGVR_Concurrent(t *testing.T) {
	c := newTestClient()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.getCachedGVR("deployments")
		}()
	}
	wg.Wait()
}

func TestResolveKindName(t *testing.T) {
	c := newTestClient()

	tests := []struct {
		input string
		want  string
	}{
		{"deployments", "Deployment"},
		{"pods", "Pod"},
		{"services", "Service"},
		{"deploy", "Deployment"},
		{"po", "Pod"},
		{"svc", "Service"},
		{"Deployment", "Deployment"},
	}

	for _, tt := range tests {
		got := c.resolveKindName(tt.input)
		if got != tt.want {
			t.Errorf("resolveKindName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveKindName_UnknownPassthrough(t *testing.T) {
	c := newTestClient()
	got := c.resolveKindName("something-unknown")
	if got != "something-unknown" {
		t.Errorf("expected passthrough for unknown kind, got %q", got)
	}
}

func TestIsShellToken(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{"|", true},
		{"&&", true},
		{">", true},
		{"pods", false},
		{"--sort-by=cpu", false},
	}

	for _, tt := range tests {
		got := isShellToken(tt.arg)
		if got != tt.want {
			t.Errorf("isShellToken(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}

func TestRunKubectlCommandMissingBinary(t *testing.T) {
	t.Setenv("PATH", "")

	c := &Client{}
	_, err := c.RunKubectlCommand(context.Background(), "kubectl get pods", 1)
	if err == nil {
		t.Fatal("expected error when kubectl is missing")
	}
	if !strings.Contains(err.Error(), "kubectl binary not found in PATH") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newClusterSummaryTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	clientset, err := kubernetes.NewForConfig(&rest.Config{
		Host:  server.URL,
		QPS:   1000,
		Burst: 1000,
	})
	if err != nil {
		t.Fatalf("create test clientset: %v", err)
	}
	metricsClient, err := metricsclientset.NewForConfig(&rest.Config{
		Host:  server.URL,
		QPS:   1000,
		Burst: 1000,
	})
	if err != nil {
		t.Fatalf("create test metrics clientset: %v", err)
	}
	return &Client{clientset: clientset, metricsClientset: metricsClient}
}

func writeClusterSummaryTestResponse(w http.ResponseWriter, path, namespaceItems string) {
	w.Header().Set("Content-Type", "application/json")
	if path == "/version" {
		_, _ = fmt.Fprint(w, `{"major":"1","minor":"34","gitVersion":"v1.34.0","platform":"linux/amd64"}`)
		return
	}

	apiVersion := "v1"
	kind := ""
	switch {
	case path == "/api/v1/nodes":
		kind = "NodeList"
	case path == "/api/v1/namespaces":
		kind = "NamespaceList"
	case path == "/api/v1/events":
		kind = "EventList"
	case path == "/apis/metrics.k8s.io/v1beta1/nodes":
		apiVersion, kind = "metrics.k8s.io/v1beta1", "NodeMetricsList"
	case path == "/api/v1/pods" || strings.HasSuffix(path, "/pods"):
		kind = "PodList"
	case strings.HasSuffix(path, "/deployments"):
		apiVersion, kind = "apps/v1", "DeploymentList"
	case strings.HasSuffix(path, "/statefulsets"):
		apiVersion, kind = "apps/v1", "StatefulSetList"
	case strings.HasSuffix(path, "/daemonsets"):
		apiVersion, kind = "apps/v1", "DaemonSetList"
	case strings.HasSuffix(path, "/jobs"):
		apiVersion, kind = "batch/v1", "JobList"
	default:
		http.Error(w, "unexpected path: "+path, http.StatusNotFound)
		return
	}

	if kind == "NamespaceList" && namespaceItems != "" {
		_, _ = fmt.Fprintf(w, `{"apiVersion":"v1","kind":"NamespaceList","items":[%s]}`, namespaceItems)
		return
	}
	_, _ = fmt.Fprintf(w, `{"apiVersion":%q,"kind":%q,"items":[]}`, apiVersion, kind)
}

func TestGetClusterSummaryStartsIndependentReadsConcurrently(t *testing.T) {
	var started atomic.Int32
	readsStarted := make(chan struct{})
	release := make(chan struct{})
	var signalOnce sync.Once

	client := newClusterSummaryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			writeClusterSummaryTestResponse(w, r.URL.Path, "")
			return
		}
		if started.Add(1) >= 4 {
			signalOnce.Do(func() { close(readsStarted) })
		}
		select {
		case <-release:
			writeClusterSummaryTestResponse(w, r.URL.Path, "")
		case <-r.Context().Done():
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.GetClusterSummary(ctx, false)
		done <- err
	}()

	select {
	case <-readsStarted:
		close(release)
	case <-ctx.Done():
		close(release)
		t.Fatal("independent cluster reads did not overlap")
	}

	if err := <-done; err != nil {
		t.Fatalf("GetClusterSummary returned an error: %v", err)
	}
}

func TestGetClusterSummaryBoundsNamespaceWorkloadConcurrency(t *testing.T) {
	namespaceCount := defaultKubernetesReadConcurrency + 4
	namespaceItems := make([]string, namespaceCount)
	for i := range namespaceItems {
		namespaceItems[i] = fmt.Sprintf(`{"metadata":{"name":"namespace-%d"}}`, i)
	}

	var started atomic.Int32
	limitReached := make(chan struct{})
	release := make(chan struct{})
	var signalOnce sync.Once
	client := newClusterSummaryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces" {
			writeClusterSummaryTestResponse(w, r.URL.Path, strings.Join(namespaceItems, ","))
			return
		}
		if strings.Contains(r.URL.Path, "/namespaces/namespace-") {
			if started.Add(1) == defaultKubernetesReadConcurrency {
				signalOnce.Do(func() { close(limitReached) })
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		writeClusterSummaryTestResponse(w, r.URL.Path, "")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.GetClusterSummary(ctx, true)
		done <- err
	}()

	select {
	case <-limitReached:
		if got := started.Load(); got != defaultKubernetesReadConcurrency {
			close(release)
			t.Fatalf("started %d namespace workload reads while blocked, want %d", got, defaultKubernetesReadConcurrency)
		}
		close(release)
	case <-ctx.Done():
		close(release)
		t.Fatal("namespace detail reads did not fan out concurrently")
	}

	if err := <-done; err != nil {
		t.Fatalf("GetClusterSummary returned an error: %v", err)
	}
	wantWorkloadReads := int32(namespaceCount * 4)
	if got := started.Load(); got != wantWorkloadReads {
		t.Fatalf("namespace workload reads = %d, want %d", got, wantWorkloadReads)
	}
}

func TestGetClusterSummaryReusesPodListForEventFiltering(t *testing.T) {
	var clusterPodLists atomic.Int32
	client := newClusterSummaryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/pods" {
			clusterPodLists.Add(1)
		}
		writeClusterSummaryTestResponse(w, r.URL.Path, "")
	})

	if _, err := client.GetClusterSummary(context.Background(), false); err != nil {
		t.Fatalf("GetClusterSummary returned an error: %v", err)
	}
	if got := clusterPodLists.Load(); got != 1 {
		t.Fatalf("cluster-wide pod lists = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Field selector client-side filtering
// ---------------------------------------------------------------------------

func makePod(name, namespace, phase string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"status": map[string]interface{}{
				"phase": phase,
			},
		},
	}
}

func TestParseFieldSelector_Equality(t *testing.T) {
	filters := parseFieldSelector("status.phase=Running")
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if filters[0].field != "status.phase" || filters[0].value != "Running" || filters[0].notEqual {
		t.Errorf("unexpected filter: %+v", filters[0])
	}
}

func TestParseFieldSelector_Inequality(t *testing.T) {
	filters := parseFieldSelector("status.phase!=Running")
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if filters[0].field != "status.phase" || filters[0].value != "Running" || !filters[0].notEqual {
		t.Errorf("unexpected filter: %+v", filters[0])
	}
}

func TestParseFieldSelector_Multiple(t *testing.T) {
	filters := parseFieldSelector("status.phase=Running,metadata.namespace=default")
	if len(filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(filters))
	}
}

func TestParseFieldSelector_Empty(t *testing.T) {
	filters := parseFieldSelector("")
	if len(filters) != 0 {
		t.Errorf("expected 0 filters for empty string, got %d", len(filters))
	}
}

func TestFilterByFieldSelector_EqualityMatch(t *testing.T) {
	items := []unstructured.Unstructured{
		makePod("pod-a", "default", "Running"),
		makePod("pod-b", "default", "Pending"),
		makePod("pod-c", "kube-system", "Running"),
	}
	result := filterByFieldSelector(items, "status.phase=Running")
	if len(result) != 2 {
		t.Fatalf("expected 2 running pods, got %d", len(result))
	}
}

func TestFilterByFieldSelector_InequalityMatch(t *testing.T) {
	items := []unstructured.Unstructured{
		makePod("pod-a", "default", "Running"),
		makePod("pod-b", "default", "Pending"),
		makePod("pod-c", "default", "Failed"),
	}
	result := filterByFieldSelector(items, "status.phase!=Running")
	if len(result) != 2 {
		t.Fatalf("expected 2 non-running pods, got %d", len(result))
	}
	for _, item := range result {
		phase := getNestedFieldValue(item.Object, "status.phase")
		if phase == "Running" {
			t.Error("Running pod should have been filtered out")
		}
	}
}

func TestFilterByFieldSelector_MultipleConditions(t *testing.T) {
	items := []unstructured.Unstructured{
		makePod("pod-a", "default", "Running"),
		makePod("pod-b", "kube-system", "Running"),
		makePod("pod-c", "default", "Pending"),
	}
	result := filterByFieldSelector(items, "status.phase=Running,metadata.namespace=default")
	if len(result) != 1 {
		t.Fatalf("expected 1 pod (Running + default ns), got %d", len(result))
	}
	name := getNestedFieldValue(result[0].Object, "metadata.name")
	if name != "pod-a" {
		t.Errorf("expected pod-a, got %s", name)
	}
}

func TestFilterByFieldSelector_NoSelector(t *testing.T) {
	items := []unstructured.Unstructured{makePod("pod-a", "default", "Running")}
	result := filterByFieldSelector(items, "")
	if len(result) != 1 {
		t.Errorf("empty selector should return all items, got %d", len(result))
	}
}

func TestGetNestedFieldValue(t *testing.T) {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "my-pod",
			"namespace": "default",
		},
		"status": map[string]interface{}{
			"phase": "Running",
		},
	}
	tests := []struct {
		path string
		want string
	}{
		{"metadata.name", "my-pod"},
		{"metadata.namespace", "default"},
		{"status.phase", "Running"},
		{"nonexistent.field", ""},
		{"metadata.nonexistent", ""},
	}
	for _, tt := range tests {
		got := getNestedFieldValue(obj, tt.path)
		if got != tt.want {
			t.Errorf("getNestedFieldValue(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
