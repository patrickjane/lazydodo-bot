FROM scratch

WORKDIR /

ARG TARGETARCH
ARG VERSION

LABEL org.opencontainers.image.version=${VERSION}

COPY lazydodobot.linux-${TARGETARCH} /lazydodobot

COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["/lazydodobot"]
