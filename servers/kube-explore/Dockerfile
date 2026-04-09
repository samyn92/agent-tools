FROM golang:1.26 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o kube-explore .

FROM scratch
COPY --from=builder /app/kube-explore /bin/kube-explore
COPY manifest.json /manifest.json
