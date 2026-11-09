BINARY   := k8s-mcp-server
SRC_DIR  := src
BUILD    := CGO_ENABLED=0 go build -buildvcs=false -ldflags="-w -s"

.PHONY: build test vet fmt fmt-check lint check docker docker-push validate clean

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
	@if command -v golangci-lint >/dev/null 2>&1; then \
		cd $(SRC_DIR) && golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

check: fmt-check vet test build

validate: check validate-chart

docker:
	$(MAKE) -C .. images-build IMAGE_BUILD_SERVICES=k8s-mcp-server-read-only

docker-push:
	$(MAKE) -C .. images-push IMAGE_BUILD_SERVICES=k8s-mcp-server-read-only

clean:
	rm -f $(SRC_DIR)/$(BINARY)

SERVICE_RELEASE := k8s-mcp-server
include ../scripts/service-chart.mk
