FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

RUN apk --no-cache upgrade && apk --no-cache add git ca-certificates

WORKDIR /app
COPY go.mod go.sum* ./
ENV GOTOOLCHAIN=auto
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-w -s" -o unifi-network-mcp-server main.go

FROM alpine:3.21

RUN apk --no-cache add ca-certificates curl tzdata && \
    addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

COPY --from=builder /app/unifi-network-mcp-server /usr/local/bin/unifi-network-mcp-server
RUN chmod +x /usr/local/bin/unifi-network-mcp-server

USER appuser

EXPOSE 8080

ENV SERVER_MODE=streamable-http
ENV SERVER_PORT=8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=5 \
    CMD curl -f http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/unifi-network-mcp-server"]
