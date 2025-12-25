# Dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY ../go.mod go.sum ./
RUN go mod download
COPY .. .
RUN go build -o rdb-analyzer cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/rdb-analyzer .
ENTRYPOINT ["./rdb-analyzer"]