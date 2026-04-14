FROM golang:1.26 AS builder
WORKDIR /app
COPY tempo/go.mod tempo/go.sum ./
COPY pkg/otelutil/ /app/pkg/otelutil/
RUN go mod download
COPY tempo/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-tempo .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/mcp-tempo /bin/mcp-tempo
COPY tempo/manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-tempo"]
