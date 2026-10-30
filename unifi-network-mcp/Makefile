.PHONY: build test run docker-build docker-push validate

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

validate: test build validate-chart

SERVICE_RELEASE := unifi-network-mcp-server
include ../scripts/service-chart.mk
