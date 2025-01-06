# syntax=docker/dockerfile:1.4

# ========================================
# Stage 1: Backend Services Compilation
# ========================================
FROM golang:1.23-alpine AS api-builder

# Static binary compilation settings
ENV CGO_ENABLED=1
RUN apk add --no-cache gcc musl-dev

WORKDIR /app/backend

# Application source code
COPY backend/ .

# Fetch Go module dependencies (cached for faster rebuilds)
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

# Compile main application binary with embedded version metadata
RUN go build -trimpath -o /pentagi ./cmd/pentagi

# ========================================
# Stage 2: Production Runtime Environment
# ========================================
FROM alpine:3.21

# Create non-root user and docker group with specific GID
RUN addgroup -g 998 docker && \
    addgroup -S pentagi && \
    adduser -S pentagi -G pentagi && \
    addgroup pentagi docker

# Install required packages
RUN apk --no-cache add ca-certificates openssl shadow && \
    rm -rf /var/cache/apk/*

ADD entrypoint.sh /opt/pentagi/bin/

RUN chmod +x /opt/pentagi/bin/entrypoint.sh

RUN mkdir -p \
    /opt/pentagi/bin \
    /opt/pentagi/ssl \
    /opt/pentagi/fe \
    /opt/pentagi/logs \
    /opt/pentagi/data

COPY --from=api-builder /pentagi /opt/pentagi/bin/pentagi

COPY LICENSE /opt/pentagi/LICENSE
COPY NOTICE /opt/pentagi/NOTICE
COPY EULA.md /opt/pentagi/EULA
COPY EULA.md /opt/pentagi/fe/EULA.md

RUN chown -R pentagi:pentagi /opt/pentagi

WORKDIR /opt/pentagi

USER pentagi

ENTRYPOINT ["/opt/pentagi/bin/entrypoint.sh", "/opt/pentagi/bin/pentagi"]

# Image Metadata
LABEL org.opencontainers.image.source="https://github.com/vxcontrol/pentagi"
LABEL org.opencontainers.image.description="Fully autonomous AI Agents system capable of performing complex penetration testing tasks"
LABEL org.opencontainers.image.authors="PentAGI Development Team"
LABEL org.opencontainers.image.licenses="MIT License"
