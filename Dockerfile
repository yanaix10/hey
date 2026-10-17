FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY cmd/hey-relay/main.go .
RUN go mod init relay && CGO_ENABLED=0 GOOS=linux go build -o hey-relay .

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/hey-relay .
EXPOSE 4244
CMD ["./hey-relay"]
