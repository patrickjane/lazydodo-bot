# --- Stage 1: Builder ---
FROM alpine:latest AS builder

# Install tzdata and CA certificates
# We need these because 'scratch' is empty
RUN apk update && apk add --no-cache tzdata ca-certificates

# --- Stage 2: Final (Scratch) ---
FROM scratch

# 1. Copy the timezone data from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# 2. Copy SSL certificates so the bot can connect to Discord (HTTPS)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /

ARG TARGETARCH
ARG VERSION

LABEL org.opencontainers.image.version=${VERSION}

COPY lazydodobot.linux-${TARGETARCH} /lazydodobot

# Set the timezone environment variable (Optional but helpful)
ENV TZ=Europe/Berlin

ENTRYPOINT ["/lazydodobot"]
