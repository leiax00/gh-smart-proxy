FROM golang:1.22-alpine AS builder
ARG USE_MIRROR=false
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN if [ "$USE_MIRROR" = "true" ]; then export GOPROXY=https://goproxy.io GOSUMDB=off; fi && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/gh-smart-proxy ./cmd/gh-smart-proxy

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /out/gh-smart-proxy /usr/local/bin/gh-smart-proxy
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gh-smart-proxy"]
