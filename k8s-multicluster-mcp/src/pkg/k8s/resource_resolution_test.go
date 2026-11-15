package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func discoveryFixture(t *testing.T, failGroup *atomic.Bool, reverse bool) (*Client, *atomic.Int32, *[]string) {
	t.Helper()
	reads := &atomic.Int32{}
	deletes := &[]string{}
	groups := []string{"metrics.k8s.io", "one.example", "two.example"}
	if reverse {
		groups[1], groups[2] = groups[2], groups[1]
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			*deletes = append(*deletes, r.URL.Path)
			if r.URL.Path != "/api/v1/namespaces/test/pods/sample" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Success"}`)
			return
		}
		switch r.URL.Path {
		case "/api":
			_ = json.NewEncoder(w).Encode(metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{"v1"}})
		case "/apis":
			list := metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}}
			for _, group := range groups {
				version := metav1.GroupVersionForDiscovery{GroupVersion: group + "/v1", Version: "v1"}
				list.Groups = append(list.Groups, metav1.APIGroup{Name: group, Versions: []metav1.GroupVersionForDiscovery{version}, PreferredVersion: version})
			}
			_ = json.NewEncoder(w).Encode(list)
		case "/api/v1":
			reads.Add(1)
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{
				{Name: "pods", SingularName: "pod", Kind: "Pod", Namespaced: true, ShortNames: []string{"po"}, Verbs: []string{"get", "list", "delete"}},
				{Name: "pods/status", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "patch", "update"}},
			}})
		case "/apis/metrics.k8s.io/v1":
			if failGroup.Load() {
				http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: "metrics.k8s.io/v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "PodMetrics", Namespaced: true, Verbs: []string{"get", "list"}}}})
		case "/apis/one.example/v1", "/apis/two.example/v1":
			group := strings.TrimPrefix(r.URL.Path, "/apis/")
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: group, APIResources: []metav1.APIResource{{Name: "widgets", SingularName: "widget", Kind: "Widget", Verbs: []string{"get", "delete"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	cfg := &rest.Config{Host: server.URL, QPS: 1000, Burst: 1000}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dy, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{discoveryClient: dc, dynamicClient: dy}, reads, deletes
}

func TestResourceResolutionAcrossGroups(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%v", reverse), func(t *testing.T) {
			client, reads, deletes := discoveryFixture(t, &atomic.Bool{}, reverse)
			for _, name := range []string{"pods", "Pod", "pod", "po", "PODS"} {
				for range 2 {
					gvr, err := client.getCachedGVR(name)
					if err != nil || gvr.Group != "" || gvr.Resource != "pods" {
						t.Fatalf("%s resolved to %v: %v", name, gvr, err)
					}
				}
			}
			for _, name := range []string{"pods.metrics.k8s.io", "PodMetrics"} {
				gvr, err := client.getCachedGVR(name)
				if err != nil || gvr.Group != "metrics.k8s.io" {
					t.Fatalf("%s resolved to %v: %v", name, gvr, err)
				}
			}
			for range 2 {
				_, err := client.getCachedGVR("widgets")
				if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "widgets.one.example, widgets.two.example") {
					t.Fatalf("expected deterministic ambiguity, got %v", err)
				}
			}
			for _, group := range []string{"one.example", "two.example"} {
				gvr, err := client.getCachedGVR("widgets." + group)
				if err != nil || gvr.Group != group {
					t.Fatalf("qualified name resolved to %v: %v", gvr, err)
				}
			}
			for _, name := range []string{"pods/status", "nonexistent"} {
				if _, err := client.getCachedGVR(name); err == nil {
					t.Fatalf("unexpected resolution for %q", name)
				}
			}
			if err := client.DeleteResource(context.Background(), "pods", "sample", "test"); err != nil {
				t.Fatal(err)
			}
			if len(*deletes) != 1 || (*deletes)[0] != "/api/v1/namespaces/test/pods/sample" {
				t.Fatalf("unexpected deletion paths: %v", *deletes)
			}
			if reads.Load() != 2 {
				t.Fatalf("expected initial discovery and one refresh on a miss, got %d", reads.Load())
			}
		})
	}
}

func TestResourceResolutionDoesNotCachePartialDiscovery(t *testing.T) {
	failing := &atomic.Bool{}
	failing.Store(true)
	client, _, _ := discoveryFixture(t, failing, false)
	if _, err := client.getCachedGVR("pods"); err == nil {
		t.Fatal("partial discovery must fail")
	}
	if !client.cacheRefreshedAt.IsZero() || len(client.apiResourceCache) != 0 {
		t.Fatal("partial discovery published cache")
	}
	failing.Store(false)
	if _, err := client.getCachedGVR("pods"); err != nil {
		t.Fatal(err)
	}
	client.cacheRefreshedAt = time.Now().Add(-2 * gvrCacheTTL)
	failing.Store(true)
	if _, err := client.getCachedGVR("pods"); err == nil {
		t.Fatal("failed refresh must fail")
	}
	if time.Since(client.cacheRefreshedAt) < gvrCacheTTL {
		t.Fatal("failed refresh extended cache lifetime")
	}
	failing.Store(false)
	if gvr, err := client.getCachedGVR("pods"); err != nil || gvr.Group != "" {
		t.Fatalf("refresh: %v %v", gvr, err)
	}
}
