// Package main is the entry point for the Kubernetes MCP server.
// Manage Kubernetes Cluster workloads via MCP.
// It initializes the MCP server, sets up the Kubernetes client,
// and registers the necessary handlers for various Kubernetes operations.
// It also starts the server to listen for incoming requests via stdio, SSE, or streamable-http transport.
// It uses the MCP Go library to create the server and handle requests.
// The server is capable of handling various Kubernetes operations
// such as listing resources, getting resource details, and retrieving logs.

package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tuttlebr/kubernetes-mcp-server/handlers"
	"github.com/tuttlebr/kubernetes-mcp-server/pkg/agent"
	"github.com/tuttlebr/kubernetes-mcp-server/pkg/helm"
	"github.com/tuttlebr/kubernetes-mcp-server/pkg/k8s"
	"github.com/tuttlebr/kubernetes-mcp-server/tools"
)

// loggingMiddleware wraps an http.Handler and logs each request
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Call the next handler
		next.ServeHTTP(rw, r)

		// Log the request
		log.Printf("[%s] %s %s - %d (%s)",
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			rw.statusCode,
			time.Since(start),
		)
	})
}

func authMiddleware(next http.Handler, tokens []string) http.Handler {
	if len(tokens) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if authorizedRequest(r, tokens...) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="k8s-mcp-server"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func authorizedRequest(r *http.Request, tokens ...string) bool {
	authHeader := r.Header.Get("Authorization")
	provided := r.Header.Get("X-MCP-Token")
	if strings.HasPrefix(authHeader, "Bearer ") {
		provided = strings.TrimPrefix(authHeader, "Bearer ")
	}
	for _, token := range tokens {
		if constantTimeEqual(provided, token) {
			return true
		}
	}
	return false
}

func constantTimeEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func addAuditedTool(s *server.MCPServer, tool mcp.Tool, capability string, handler server.ToolHandlerFunc) {
	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		result, err := handler(ctx, request)
		status := "ok"
		if err != nil {
			status = "error"
		} else if result != nil && result.IsError {
			status = "tool_error"
		}
		if err != nil {
			log.Printf("mcp_tool_call tool=%s capability=%s status=%s duration=%s error=%q",
				tool.Name, capability, status, time.Since(start), err.Error())
		} else {
			log.Printf("mcp_tool_call tool=%s capability=%s status=%s duration=%s",
				tool.Name, capability, status, time.Since(start))
		}
		return result, err
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code
// while preserving Flusher/Hijacker interfaces required for SSE streaming.
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// main initializes the Kubernetes client, sets up the MCP server with
// Kubernetes tool handlers, and starts the server in the configured mode.
func main() {
	// Parse command line flags
	var mode string
	var port string
	var readOnly bool
	var noK8s bool
	var noHelm bool
	var noAgent bool
	var enableExec bool
	var enableKubectl bool
	var enableAgentWrite bool

	flag.StringVar(&port, "port", getEnvOrDefault("SERVER_PORT", "8080"), "Server port")
	flag.StringVar(&mode, "mode", getEnvOrDefault("SERVER_MODE", "sse"), "Server mode: 'stdio', 'sse', or 'streamable-http'")
	flag.BoolVar(&readOnly, "read-only", getEnvBoolDefault("SERVER_READ_ONLY", false), "Enable read-only mode (disables write operations)")
	flag.BoolVar(&noK8s, "no-k8s", false, "Disable Kubernetes tools")
	flag.BoolVar(&noHelm, "no-helm", getEnvBoolDefault("SERVER_NO_HELM", false), "Disable Helm tools")
	flag.BoolVar(&noAgent, "no-agent", false, "Disable DevOps agent tool")
	flag.BoolVar(&enableExec, "enable-exec", getEnvBoolDefault("MCP_ENABLE_EXEC", false), "Enable execInPod tool when not in read-only mode")
	flag.BoolVar(&enableKubectl, "enable-kubectl", getEnvBoolDefault("MCP_ENABLE_KUBECTL", false), "Enable runKubectlCommand tool when not in read-only mode")
	flag.BoolVar(&enableAgentWrite, "enable-agent-write", getEnvBoolDefault("MCP_ENABLE_AGENT_WRITE", false), "Allow devopsAgent to run write-capable child MCP sessions when server is not read-only")
	flag.Parse()

	authTokens := nonEmptyStrings(
		os.Getenv("MCP_AUTH_TOKEN"),
		os.Getenv("MCP_AUTH_TOKEN_NEXT"),
	)
	requireAuth := getEnvBoolDefault("MCP_REQUIRE_AUTH", false)
	if mode != "stdio" && requireAuth && len(authTokens) == 0 {
		log.Println("Error: MCP_REQUIRE_AUTH=true requires MCP_AUTH_TOKEN or MCP_AUTH_TOKEN_NEXT to be set")
		os.Exit(1)
	}

	// Validate flag combinations
	if noK8s && noHelm {
		log.Println("Error: Cannot disable both Kubernetes and Helm tools. At least one tool category must be enabled.")
		os.Exit(1)
	}

	// Log read-only mode status
	if readOnly {
		log.Println("Starting server in read-only mode - write operations disabled")
	}

	// Log disabled tool categories
	if noK8s {
		log.Println("Kubernetes tools disabled")
	}
	if noHelm {
		log.Println("Helm tools disabled")
	}
	if noAgent {
		log.Println("DevOps agent disabled")
	}
	if mode != "stdio" && len(authTokens) == 0 {
		log.Println("Warning: HTTP MCP auth is disabled; set MCP_AUTH_TOKEN or MCP_REQUIRE_AUTH=true for shared environments")
	}
	if !readOnly {
		log.Println("Write-capable mode enabled")
		if !enableExec {
			log.Println("execInPod disabled; set --enable-exec or MCP_ENABLE_EXEC=true to expose it")
		}
		if !enableKubectl {
			log.Println("runKubectlCommand disabled; set --enable-kubectl or MCP_ENABLE_KUBECTL=true to expose it")
		}
		if !enableAgentWrite {
			log.Println("DevOps agent write mode disabled; set --enable-agent-write or MCP_ENABLE_AGENT_WRITE=true to allow it")
		}
	}

	// Create MCP server
	s := server.NewMCPServer(
		"MCP K8S & Helm Server",
		"1.0.0",
		server.WithResourceCapabilities(true, true), // Enable resource listing and subscription capabilities
	)

	// Create a Kubernetes client
	client, err := k8s.NewClient("")
	if err != nil {
		log.Printf("Failed to create Kubernetes client: %v", err)
		return
	}

	// Create Helm client with default kubeconfig path
	helmClient, err := helm.NewClient("")
	if err != nil {
		log.Printf("Failed to create Helm client: %v", err)
		return
	}

	// Create agent client (optional — only if OPENCODE_BASE_URL is configured)
	var agentClient *agent.Client
	if !noAgent && os.Getenv("OPENCODE_BASE_URL") != "" {
		agentClient, err = agent.NewClient(client.KubeconfigPath())
		if err != nil {
			log.Printf("Warning: Failed to create agent client: %v", err)
			log.Println("DevOps agent tool will not be available")
		} else {
			log.Println("DevOps agent enabled (opencode integration)")
		}
	}

	// Register Kubernetes tools
	if !noK8s {
		addAuditedTool(s, tools.GetAPIResourcesTool(), "read", handlers.GetAPIResources(client))
		addAuditedTool(s, tools.ListResourcesTool(), "read", handlers.ListResources(client))
		addAuditedTool(s, tools.GetResourcesTool(), "read", handlers.GetResources(client))
		addAuditedTool(s, tools.DescribeResourcesTool(), "read", handlers.DescribeResources(client))
		addAuditedTool(s, tools.GetPodsLogsTools(), "read/logs", handlers.GetPodsLogs(client))
		addAuditedTool(s, tools.GetNodeMetricsTools(), "read", handlers.GetNodeMetrics(client))
		addAuditedTool(s, tools.GetPodMetricsTool(), "read", handlers.GetPodMetrics(client))
		addAuditedTool(s, tools.GetEventsTool(), "read", handlers.GetEvents(client))
		addAuditedTool(s, tools.GetIngressesTool(), "read", handlers.GetIngresses(client))

		// Enhanced Resource Inspection Tools (Read-Only)
		addAuditedTool(s, tools.GetResourceYAMLTool(), "read", handlers.GetResourceYAML(client))
		addAuditedTool(s, tools.GetResourceDiffTool(), "read", handlers.GetResourceDiff(client))
		addAuditedTool(s, tools.GetNamespaceResourcesTool(), "read", handlers.GetNamespaceResources(client))
		addAuditedTool(s, tools.GetResourceOwnersTool(), "read", handlers.GetResourceOwners(client))

		// Advanced Monitoring & Observability Tools (Read-Only)
		addAuditedTool(s, tools.GetClusterHealthTool(), "read", handlers.GetClusterHealth(client))
		addAuditedTool(s, tools.GetResourceQuotasTool(), "read", handlers.GetResourceQuotas(client))
		addAuditedTool(s, tools.GetLimitRangesTool(), "read", handlers.GetLimitRanges(client))
		addAuditedTool(s, tools.GetTopPodsTool(), "read", handlers.GetTopPods(client))
		addAuditedTool(s, tools.GetTopNodesTool(), "read", handlers.GetTopNodes(client))

		// Debugging & Troubleshooting Tools (Read-Only)
		addAuditedTool(s, tools.GetPodDebugInfoTool(), "read/logs", handlers.GetPodDebugInfo(client))
		addAuditedTool(s, tools.GetServiceEndpointsTool(), "read", handlers.GetServiceEndpoints(client))
		addAuditedTool(s, tools.GetNetworkPoliciesTool(), "read", handlers.GetNetworkPolicies(client))
		addAuditedTool(s, tools.GetSecurityContextTool(), "read", handlers.GetSecurityContext(client))
		addAuditedTool(s, tools.GetResourceHistoryTool(), "read", handlers.GetResourceHistory(client))
		if !readOnly {
			addAuditedTool(s, tools.ValidateManifestTool(), "write/dry-run", handlers.ValidateManifest(client))
		}

		// Cluster Overview (Read-Only)
		addAuditedTool(s, tools.GetClusterSummaryTool(), "read", handlers.GetClusterSummary(client))

		// GPU Debugging & Troubleshooting (Read-Only)
		addAuditedTool(s, tools.GetGPUClusterOverviewTool(), "read", handlers.GetGPUClusterOverview(client))
		addAuditedTool(s, tools.DiagnoseGPUSchedulingTool(), "read", handlers.DiagnoseGPUScheduling(client))
		addAuditedTool(s, tools.GetGPUOperatorHealthTool(), "read/logs", handlers.GetGPUOperatorHealth(client))

		// Namespace & cluster navigation (Read-Only)
		addAuditedTool(s, tools.ListNamespacesTool(), "read", handlers.ListNamespaces(client))
		addAuditedTool(s, tools.GetRolloutStatusTool(), "read", handlers.GetRolloutStatus(client))
		addAuditedTool(s, tools.ListContextsTool(), "read", handlers.ListContexts(client))

		if !readOnly {
			addAuditedTool(s, tools.CreateOrUpdateResourceJSONTool(), "write", handlers.CreateOrUpdateResourceJSON(client))
			addAuditedTool(s, tools.CreateOrUpdateResourceYAMLTool(), "write", handlers.CreateOrUpdateResourceYAML(client))
			addAuditedTool(s, tools.DeleteResourceTool(), "write/destructive", handlers.DeleteResource(client))
			addAuditedTool(s, tools.RolloutRestartTool(), "write", handlers.RolloutRestart(client))
			addAuditedTool(s, tools.ScaleResourceTool(), "write", handlers.ScaleResource(client))

			// GPU Remediation (Write)
			addAuditedTool(s, tools.RemediateGPUIssueTool(), "write", handlers.RemediateGPUIssue(client))

			if enableExec {
				addAuditedTool(s, tools.ExecInPodTool(), "exec", handlers.ExecInPod(client))
			}

			if enableKubectl {
				addAuditedTool(s, tools.RunKubectlCommandTool(), "kubectl", handlers.RunKubectlCommand(client))
			}
		}

		// DevOps Agent (requires opencode CLI and env vars). In parent
		// read-only mode, or without explicit agent-write capability, the
		// handler forces inspection-only child runs.
		if agentClient != nil {
			addAuditedTool(s, tools.DevopsAgentTool(), "agent", handlers.DevopsAgent(agentClient, readOnly || !enableAgentWrite))
		}
	}

	// Register Helm tools
	if !noHelm {
		addAuditedTool(s, tools.HelmListTool(), "read", handlers.HelmList(helmClient))
		addAuditedTool(s, tools.HelmGetTool(), "read", handlers.HelmGet(helmClient))
		addAuditedTool(s, tools.HelmHistoryTool(), "read", handlers.HelmHistory(helmClient))
		addAuditedTool(s, tools.HelmRepoListTool(), "read", handlers.HelmRepoList(helmClient))

		// Register write operations only if not in read-only mode
		if !readOnly {
			addAuditedTool(s, tools.HelmInstallTool(), "write", handlers.HelmInstall(helmClient))
			addAuditedTool(s, tools.HelmUpgradeTool(), "write", handlers.HelmUpgrade(helmClient))
			addAuditedTool(s, tools.HelmUninstallTool(), "write/destructive", handlers.HelmUninstall(helmClient))
			addAuditedTool(s, tools.HelmRollbackTool(), "write", handlers.HelmRollback(helmClient))
			addAuditedTool(s, tools.HelmRepoAddTool(), "write", handlers.HelmRepoAdd(helmClient))
		}
	}

	// Set up signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch mode {
	case "stdio":
		log.Println("Starting server in stdio mode...")
		if err := server.ServeStdio(s); err != nil {
			log.Printf("Failed to start stdio server: %v", err)
			return
		}
	case "sse":
		log.Printf("Starting server in SSE mode on port %s...", port)
		httpServer := &http.Server{
			Addr:              ":" + port,
			ReadHeaderTimeout: 30 * time.Second,
			ReadTimeout:       5 * time.Minute,
			IdleTimeout:       5 * time.Minute,
		}
		sse := server.NewSSEServer(s,
			server.WithHTTPServer(httpServer),
			server.WithKeepAliveInterval(30*time.Second),
		)

		baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isHealthPath(r.URL.Path) {
				handleHealth(w, r, client)
				return
			}
			sse.ServeHTTP(w, r)
		})

		httpServer.Handler = loggingMiddleware(authMiddleware(baseHandler, authTokens))

		go func() {
			if err := sse.Start(":" + port); err != nil && err != http.ErrServerClosed {
				log.Printf("SSE server error: %v", err)
			}
		}()
		log.Printf("SSE server started on port %s", port)

		<-ctx.Done()
		log.Println("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}

	case "streamable-http":
		log.Printf("Starting server in streamable-http mode on port %s...", port)
		httpServer := &http.Server{
			Addr:              ":" + port,
			ReadHeaderTimeout: 30 * time.Second,
			ReadTimeout:       5 * time.Minute,
			IdleTimeout:       5 * time.Minute,
		}
		streamableHTTP := server.NewStreamableHTTPServer(
			s,
			server.WithStreamableHTTPServer(httpServer),
			server.WithHeartbeatInterval(30*time.Second),
		)

		baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isHealthPath(r.URL.Path) {
				handleHealth(w, r, client)
				return
			}
			streamableHTTP.ServeHTTP(w, r)
		})

		httpServer.Handler = loggingMiddleware(authMiddleware(baseHandler, authTokens))

		go func() {
			if err := streamableHTTP.Start(":" + port); err != nil && err != http.ErrServerClosed {
				log.Printf("Streamable-http server error: %v", err)
			}
		}()
		log.Printf("Streamable-http server started on port %s (endpoint: http://localhost:%s/mcp)", port, port)

		<-ctx.Done()
		log.Println("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}

	default:
		log.Printf("Unknown server mode: %s. Use 'stdio', 'sse', or 'streamable-http'.", mode)
		return
	}

	log.Println("Server stopped gracefully")
}

// getEnvOrDefault returns the value of the environment variable or the default value if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvBoolDefault(key string, defaultValue bool) bool {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		log.Printf("Warning: invalid boolean value for %s=%q; using default %v", key, value, defaultValue)
		return defaultValue
	}
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func isHealthPath(path string) bool {
	switch path {
	case "/healthz", "/livez", "/readyz", "/dependencyz":
		return true
	default:
		return false
	}
}

// handleHealth keeps process readiness independent from Kubernetes API health.
// /healthz remains as a compatibility alias for /readyz.
func handleHealth(w http.ResponseWriter, r *http.Request, client *k8s.Client) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := map[string]string{"status": "ok"}
	status := http.StatusOK
	if r.URL.Path == "/dependencyz" {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := client.CheckConnection(ctx); err != nil {
			status = http.StatusServiceUnavailable
			response["status"] = "unavailable"
			response["kubernetesApi"] = err.Error()
		} else {
			response["kubernetesApi"] = "ok"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
