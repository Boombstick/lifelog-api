FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app
COPY --from=builder /out/ /app/
RUN mkdir -p /app/secrets

ENTRYPOINT ["/app/api"]
