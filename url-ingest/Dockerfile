FROM rust:1.90.0-alpine3.21 AS build
ARG TARGETARCH
ENV PATH="/usr/local/cargo/bin:${PATH}"

WORKDIR /app

RUN apk add --no-cache build-base

# Add rustup target for musl (match build platform)
RUN case "${TARGETARCH}" in \
    "amd64") RUST_TARGET="x86_64-unknown-linux-musl" ;; \
    "arm64") RUST_TARGET="aarch64-unknown-linux-musl" ;; \
    *) echo "Unsupported arch: ${TARGETARCH}" && exit 1 ;; \
  esac && \
  rustup target add "${RUST_TARGET}" && \
  echo "${RUST_TARGET}" > /tmp/rust_target

COPY Cargo.toml Cargo.lock* ./
RUN mkdir src && echo "fn main() {}" > src/main.rs
RUN cargo build --release --target "$(cat /tmp/rust_target)"

COPY src ./src
# Touch main.rs to force rebuild
RUN touch src/main.rs
RUN cargo build --release --target "$(cat /tmp/rust_target)" && \
  cp "/app/target/$(cat /tmp/rust_target)/release/url_ingest" /app/url_ingest

FROM build AS check

RUN rustup component add rustfmt clippy && \
  cargo fmt --check && \
  cargo clippy --target "$(cat /tmp/rust_target)" -- -D warnings && \
  cargo test --target "$(cat /tmp/rust_target)"

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
  addgroup -S app && \
  adduser -S -G app app

WORKDIR /app

COPY --from=build /app/url_ingest /usr/local/bin/url_ingest

USER app

CMD ["url_ingest"]
