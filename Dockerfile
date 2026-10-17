# Stage 1: Build static Go binary
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/hey-relay ./cmd/hey-relay
RUN CGO_ENABLED=0 GOOS=linux go build -o hey-relay ./cmd/hey-relay

# Stage 2: Minimal runtime image
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/hey-relay .
EXPOSE 4244
CMD ["./hey-relay"]