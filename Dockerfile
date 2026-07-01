FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY config/ ./config/
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/gh-smart-proxy .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /out/gh-smart-proxy /usr/local/bin/gh-smart-proxy
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gh-smart-proxy"]
