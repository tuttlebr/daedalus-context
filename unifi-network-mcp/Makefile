.PHONY: build test run docker-build docker-push validate

build:
	cd src && CGO_ENABLED=0 go build -o unifi-network-mcp-server main.go

test:
	cd src && CGO_ENABLED=0 go test ./...

run:
	cd src && go run . --mode streamable-http --port $${SERVER_PORT:-8080}

docker-build:
	$(MAKE) -C .. images-build IMAGE_BUILD_SERVICES=unifi-network-mcp-server

docker-push:
	$(MAKE) -C .. images-push IMAGE_BUILD_SERVICES=unifi-network-mcp-server

validate: test build validate-chart

SERVICE_RELEASE := unifi-network-mcp-server
include ../scripts/service-chart.mk
