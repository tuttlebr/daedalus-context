package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tuttlebr/unifi-network-mcp/handlers"
	"github.com/tuttlebr/unifi-network-mcp/pkg/unifi"
	"github.com/tuttlebr/unifi-network-mcp/tools"
)

func main() {
	var mode string
	var port string

	flag.StringVar(&port, "port", getEnvOrDefault("SERVER_PORT", "8080"), "Server port")
	flag.StringVar(&mode, "mode", getEnvOrDefault("SERVER_MODE", "streamable-http"), "Server mode: 'stdio', 'sse', or 'streamable-http'")
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
	if mode != "stdio" && len(authTokens) == 0 {
		log.Println("Warning: HTTP MCP auth is disabled; set MCP_AUTH_TOKEN or MCP_REQUIRE_AUTH=true for shared environments")
	}

	clientDefaults := clientDefaultsFromEnv()
	toolDefaults := toolDefaultsFromClientDefaults(clientDefaults)
	logDefaultStatus(clientDefaults)
	dependencyCheck, err := dependencyCheckerFromDefaults(clientDefaults)
	if err != nil {
		log.Printf("Error: invalid fixed UniFi dependency configuration: %v", err)
		os.Exit(1)
	}

	s := server.NewMCPServer(
		"UniFi Network MCP Server",
		"0.2.0",
		server.WithResourceCapabilities(true, true),
	)

	endpoints := unifi.Endpoints()
	readOnlyCount := 0
	for _, endpoint := range endpoints {
		if endpoint.IsReadOnly() {
			readOnlyCount++
		}
		addAuditedTool(s, tools.EndpointTool(endpoint, toolDefaults), strings.ToLower(endpoint.Method), handlers.EndpointHandler(endpoint, clientDefaults))
	}
	log.Printf("Registered %d UniFi tools (%d read-only, %d mutation)", len(endpoints), readOnlyCount, len(endpoints)-readOnlyCount)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch mode {
	case "stdio":
		log.Println("Starting server in stdio mode")
		if err := server.ServeStdio(s); err != nil {
			log.Printf("Failed to start stdio server: %v", err)
			os.Exit(1)
		}
	case "sse":
		startSSE(ctx, s, port, authTokens, dependencyCheck)
	case "streamable-http":
		startStreamableHTTP(ctx, s, port, authTokens, dependencyCheck)
	default:
		log.Printf("Unknown server mode: %s. Use 'stdio', 'sse', or 'streamable-http'.", mode)
		os.Exit(1)
	}

	log.Println("Server stopped gracefully")
}

type dependencyChecker func(context.Context) error

func startSSE(
	ctx context.Context,
	s *server.MCPServer,
	port string,
	authTokens []string,
	checkDependency dependencyChecker,
) {
	log.Printf("Starting server in SSE mode on port %s", port)
	httpServer := &http.Server{
		Addr:              ":" + port,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       5 * time.Minute,
		IdleTimeout:       5 * time.Minute,
	}
	sse := server.NewSSEServer(
		s,
		server.WithHTTPServer(httpServer),
		server.WithKeepAliveInterval(30*time.Second),
	)

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthPath(r.URL.Path) {
			handleHealth(w, r, checkDependency)
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
	waitForShutdown(ctx, httpServer)
}

func startStreamableHTTP(
	ctx context.Context,
	s *server.MCPServer,
	port string,
	authTokens []string,
	checkDependency dependencyChecker,
) {
	log.Printf("Starting server in streamable-http mode on port %s", port)
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
			handleHealth(w, r, checkDependency)
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
	waitForShutdown(ctx, httpServer)
}

func waitForShutdown(ctx context.Context, httpServer *http.Server) {
	<-ctx.Done()
	log.Println("Shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
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

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
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
		w.Header().Set("WWW-Authenticate", `Bearer realm="unifi-network-mcp"`)
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

func handleHealth(w http.ResponseWriter, r *http.Request, checkDependency dependencyChecker) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := map[string]string{"status": "ok"}
	status := http.StatusOK
	if r.URL.Path == "/dependencyz" {
		if checkDependency == nil {
			response["status"] = "unknown"
			response["unifiController"] = "not-configured"
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			if err := checkDependency(ctx); err != nil {
				status = http.StatusServiceUnavailable
				response["status"] = "unavailable"
				response["unifiController"] = err.Error()
			} else {
				response["unifiController"] = "ok"
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
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

func clientDefaultsFromEnv() handlers.ClientDefaults {
	return handlers.ClientDefaults{
		BaseURL:            strings.TrimSpace(os.Getenv("UNIFI_BASE_URL")),
		APIKey:             strings.TrimSpace(os.Getenv("UNIFI_API_KEY")),
		InsecureSkipVerify: getOptionalEnvBool("UNIFI_INSECURE_SKIP_VERIFY"),
		Locked:             getEnvBoolDefault("UNIFI_FIXED_CONTROLLER", false),
	}
}

func toolDefaultsFromClientDefaults(defaults handlers.ClientDefaults) tools.ToolDefaults {
	toolDefaults := tools.ToolDefaults{
		HasBaseURLDefault: strings.TrimSpace(defaults.BaseURL) != "",
		HasAPIKeyDefault:  strings.TrimSpace(defaults.APIKey) != "",
		ConnectionLocked:  defaults.Locked,
	}
	if defaults.InsecureSkipVerify != nil {
		toolDefaults.InsecureSkipVerifyDefaultSet = true
		toolDefaults.InsecureSkipVerifyDefault = *defaults.InsecureSkipVerify
	}
	return toolDefaults
}

func dependencyCheckerFromDefaults(defaults handlers.ClientDefaults) (dependencyChecker, error) {
	if strings.TrimSpace(defaults.BaseURL) == "" || strings.TrimSpace(defaults.APIKey) == "" {
		if defaults.Locked {
			return nil, fmt.Errorf("fixed-controller mode requires UNIFI_BASE_URL and UNIFI_API_KEY")
		}
		return nil, nil
	}
	insecureSkipVerify := false
	if defaults.InsecureSkipVerify != nil {
		insecureSkipVerify = *defaults.InsecureSkipVerify
	}
	client, err := unifi.NewClient(unifi.Config{
		BaseURL:            defaults.BaseURL,
		APIKey:             defaults.APIKey,
		Timeout:            3 * time.Second,
		InsecureSkipVerify: insecureSkipVerify,
	})
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		_, err := client.GetJSON(ctx, "/v1/info", nil)
		return err
	}, nil
}

func getOptionalEnvBool(key string) *bool {
	value, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(value) == "" {
		return nil
	}
	parsed := getEnvBoolDefault(key, false)
	return &parsed
}

func logDefaultStatus(defaults handlers.ClientDefaults) {
	if defaults.Locked {
		log.Println("UniFi fixed-controller mode enabled; per-call connection overrides are disabled")
	}
	if strings.TrimSpace(defaults.BaseURL) != "" {
		log.Println("UniFi baseUrl server default configured")
	} else {
		log.Println("UniFi baseUrl server default not configured; clients must provide baseUrl")
	}
	if strings.TrimSpace(defaults.APIKey) != "" {
		log.Println("UniFi apiKey server default configured")
	} else {
		log.Println("UniFi apiKey server default not configured; clients must provide apiKey")
	}
	if defaults.InsecureSkipVerify != nil {
		log.Printf("UniFi insecureSkipVerify server default configured: %t", *defaults.InsecureSkipVerify)
	} else {
		log.Println("UniFi insecureSkipVerify server default not configured; default is false unless supplied by client")
	}
}
