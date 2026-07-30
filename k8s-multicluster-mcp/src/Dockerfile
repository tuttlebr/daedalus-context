# Stage 1: Build the Go binary
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

# Install git (required for go modules) and update all packages
RUN apk --no-cache upgrade && apk --no-cache add git ca-certificates

# Set the working directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Set GOTOOLCHAIN to auto to allow automatic toolchain upgrades
ENV GOTOOLCHAIN=auto

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build arguments for cross-compilation
ARG TARGETOS
ARG TARGETARCH

# Build the Go app with cross-compilation support
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-w -s" -o k8s-mcp-server main.go

# Stage 2: Install opencode for DevOps agent support
FROM oven/bun:1-alpine AS opencode-installer
RUN bun install -g opencode-ai

# Stage 3: Final runtime image
FROM alpine:3.21

# Install ca-certificates, curl for health checks, tzdata for timezone,
# gcompat + libstdc++ for Bun/opencode runtime, plus the toolchain the
# DevOps-engineer skills assume in their workflows:
#   - git, github-cli  -> gh-address-comments, gh-fix-ci, security-*, debugging
#   - python3, py3-yaml -> skill-creator/jupyter-notebook/gh-* helper scripts
#   - jq               -> shell-script JSON parsing used throughout the skills
RUN apk --no-cache add \
        ca-certificates \
        curl \
        tzdata \
        gcompat \
        libstdc++ \
        bash \
        git \
        github-cli \
        python3 \
        py3-yaml \
        jq

# Install Helm for cluster management (with checksum verification)
ARG TARGETARCH
RUN case "${TARGETARCH}" in \
      amd64) HELM_ARCH=amd64; HELM_SHA256="ee88b3c851ae6466a3de507f7be73fe94d54cbf2987cbaa3d1a3832ea331f2cd" ;; \
      arm64) HELM_ARCH=arm64; HELM_SHA256="7944e3defd386c76fd92d9e6fec5c2d65a323f6fadc19bfb5e704e3eee10348e" ;; \
      *) HELM_ARCH=amd64; HELM_SHA256="ee88b3c851ae6466a3de507f7be73fe94d54cbf2987cbaa3d1a3832ea331f2cd" ;; \
    esac && \
    curl -fsSL "https://get.helm.sh/helm-v3.17.3-linux-${HELM_ARCH}.tar.gz" -o /tmp/helm.tar.gz && \
    echo "${HELM_SHA256}  /tmp/helm.tar.gz" | sha256sum -c - && \
    tar xz -C /tmp -f /tmp/helm.tar.gz && \
    mv /tmp/linux-${HELM_ARCH}/helm /usr/local/bin/helm && \
    chmod +x /usr/local/bin/helm && \
    rm -rf /tmp/helm.tar.gz /tmp/linux-${HELM_ARCH}

# Install kubectl for the runKubectlCommand MCP tool (with checksum verification)
ARG KUBECTL_VERSION=v1.34.1
RUN case "${TARGETARCH}" in \
      amd64) KUBECTL_ARCH=amd64 ;; \
      arm64) KUBECTL_ARCH=arm64 ;; \
      *) KUBECTL_ARCH=amd64 ;; \
    esac && \
    curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${KUBECTL_ARCH}/kubectl" -o /usr/local/bin/kubectl && \
    curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${KUBECTL_ARCH}/kubectl.sha256" -o /tmp/kubectl.sha256 && \
    echo "$(cat /tmp/kubectl.sha256)  /usr/local/bin/kubectl" | sha256sum -c - && \
    chmod +x /usr/local/bin/kubectl && \
    rm -f /tmp/kubectl.sha256

# Set timezone
ENV TZ=America/New_York

# Create a non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Copy the binary from the builder stage
COPY --from=builder /app/k8s-mcp-server /usr/local/bin/k8s-mcp-server
RUN chmod +x /usr/local/bin/k8s-mcp-server

# Copy Bun runtime and opencode for DevOps agent
COPY --from=opencode-installer /usr/local/bin/bun /usr/local/bin/bun
COPY --from=opencode-installer --chown=1001:1001 /root/.bun/install/global /home/appuser/.bun/install/global
RUN OPENCODE_BIN="$(find /home/appuser/.bun/install/global/node_modules -path '*/bin/opencode' -type f | grep 'linux.*musl' | head -n 1)" && \
    test -n "${OPENCODE_BIN}" && \
    ln -s "${OPENCODE_BIN}" /usr/local/bin/opencode

# Create opencode config, data, and cache directories.
RUN mkdir -p /home/appuser/.config/opencode/skills \
    /home/appuser/.local/share/opencode \
    /home/appuser/.cache/opencode && \
    chown -R appuser:appgroup /home/appuser/.config /home/appuser/.local /home/appuser/.cache

# Copy the bundled DevOps-engineer skills. Each skill's contents (SKILL.md,
# references/, scripts/, assets/) get placed under .../skills/<name>/.
# Skills can be overridden at runtime by volume-mounting over this path.
COPY --chown=appuser:appgroup skills/ /home/appuser/.config/opencode/skills/

# Switch to non-root user
USER appuser

# Expose the port the app runs on
EXPOSE 8080

# Set default environment variables
ENV SERVER_MODE=streamable-http
ENV SERVER_PORT=8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=5 \
    CMD curl -f http://localhost:8080/healthz || exit 1

# Command to run the executable
ENTRYPOINT ["/usr/local/bin/k8s-mcp-server"]
