BINARY   := k8s-mcp-server
SRC_DIR  := src
BUILD    := CGO_ENABLED=0 go build -buildvcs=false -ldflags="-w -s"

.PHONY: build test vet fmt fmt-check lint check docker docker-push render validate deploy status wait-ready smoke clean hooks

build:
	cd $(SRC_DIR) && $(BUILD) -o $(BINARY) .

test:
	cd $(SRC_DIR) && CGO_ENABLED=0 go test -buildvcs=false ./...

vet:
	cd $(SRC_DIR) && CGO_ENABLED=0 go vet -buildvcs=false ./...

fmt:
	cd $(SRC_DIR) && gofmt -w .

fmt-check:
	@cd $(SRC_DIR) && test -z "$$(gofmt -l .)" || \
		(echo "Files not formatted:"; gofmt -l .; exit 1)

lint:
	@command -v golangci-lint >/dev/null 2>&1 && \
		(cd $(SRC_DIR) && golangci-lint run) || \
		echo "golangci-lint not installed, skipping"

check: fmt-check vet test build

render:
	MCP_AUTH_TOKEN=validation \
	  K8S_MCP_IMAGE=example.invalid/k8s-mcp:0.0.0 \
	  DRY_RUN=true BUILD_IMAGE=false PUSH_IMAGE=false ./deploy.sh

validate: check render

docker:
	docker compose --project-directory .. -f ../docker-compose.yaml \
	  build k8s-mcp-server-read-only

docker-push: docker
	docker compose --project-directory .. -f ../docker-compose.yaml \
	  push k8s-mcp-server-read-only

deploy:
	./deploy.sh

status:
	kubectl --namespace="$${K8S_MCP_NAMESPACE:-k8s-mcp-server}" \
	  get deployment/k8s-mcp-server service/k8s-mcp-server \
	  poddisruptionbudget/k8s-mcp-server

wait-ready:
	kubectl --namespace="$${K8S_MCP_NAMESPACE:-k8s-mcp-server}" \
	  rollout status deployment/k8s-mcp-server --timeout=10m

smoke:
	kubectl get --raw "/api/v1/namespaces/$${K8S_MCP_NAMESPACE:-k8s-mcp-server}/services/http:k8s-mcp-server:8080/proxy/livez"

clean:
	rm -f $(SRC_DIR)/$(BINARY)

hooks:
	@echo "Installing pre-commit hook..."
	@mkdir -p .git/hooks
	@cp scripts/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Done. Hook installed at .git/hooks/pre-commit"
