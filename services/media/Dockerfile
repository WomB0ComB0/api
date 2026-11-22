FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o main ./cmd/server

FROM alpine:3.21

RUN apk --no-cache add ca-certificates curl && adduser -D -H appuser

WORKDIR /app
COPY --from=builder /app/main ./main

USER appuser
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://127.0.0.1:8080/v1/media/health || exit 1

CMD ["./main"]
