.PHONY: build test run docker-build docker-push render validate deploy status wait-ready smoke

build:
	cd src && CGO_ENABLED=0 go build -o unifi-network-mcp-server main.go

test:
	cd src && CGO_ENABLED=0 go test ./...

run:
	cd src && go run . --mode streamable-http --port $${SERVER_PORT:-8080}

docker-build:
	docker compose --project-directory .. -f ../docker-compose.yaml \
	  build unifi-network-mcp-server

docker-push: docker-build
	docker compose --project-directory .. -f ../docker-compose.yaml \
	  push unifi-network-mcp-server

render:
	MCP_AUTH_TOKEN=validation \
	  UNIFI_MCP_IMAGE=example.invalid/unifi-mcp:0.0.0 \
	  DRY_RUN=true BUILD_IMAGE=false PUSH_IMAGE=false ./deploy.sh

validate: test build render

deploy:
	./deploy.sh

status:
	kubectl --namespace="$${UNIFI_MCP_NAMESPACE:-unifi-network-mcp-server}" \
	  get deployment/unifi-network-mcp-server service/unifi-network-mcp-server \
	  poddisruptionbudget/unifi-network-mcp-server

wait-ready:
	kubectl --namespace="$${UNIFI_MCP_NAMESPACE:-unifi-network-mcp-server}" \
	  rollout status deployment/unifi-network-mcp-server --timeout=10m

smoke:
	kubectl get --raw "/api/v1/namespaces/$${UNIFI_MCP_NAMESPACE:-unifi-network-mcp-server}/services/http:unifi-network-mcp-server:8080/proxy/livez"
