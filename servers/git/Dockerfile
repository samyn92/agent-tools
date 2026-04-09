FROM golang:1.26 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-git .

FROM alpine:3.21
RUN apk add --no-cache git
COPY --from=builder /app/mcp-git /bin/mcp-git
COPY manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-git"]
