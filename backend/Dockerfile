FROM golang:1.25-bookworm AS builder

WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/txsim-server ./cmd/server

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends bash ca-certificates curl git gzip tar \
  && rm -rf /var/lib/apt/lists/*

ARG FOUNDRY_KYBER_TAG=nightly-04bdb43d9435cecec89abc17b3dcb618086c0d99
COPY scripts/install-foundry-kyber.sh /tmp/install-foundry-kyber.sh
RUN /tmp/install-foundry-kyber.sh \
  && rm /tmp/install-foundry-kyber.sh

ENV PATH="/root/.foundry/bin:${PATH}"
ENV TXSIM_CONFIG="/app/config.docker.yaml"

WORKDIR /app
COPY --from=builder /out/txsim-server /usr/local/bin/txsim-server
COPY config.docker.yaml ./config.docker.yaml
COPY contracts ./contracts
RUN mkdir -p /data/runs

EXPOSE 8080
CMD ["txsim-server"]
