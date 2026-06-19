# Stage 1: Builder
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go install github.com/swaggo/swag/cmd/swag@latest
RUN /go/bin/swag init -g cmd/server/main.go -o docs --parseInternal
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o url-shortener ./cmd/server

# Stage 2: Final image
FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
RUN mkdir -p /app/data
COPY --from=builder /app/url-shortener .
COPY --from=builder /app/migrations ./migrations
COPY .env.example .env
EXPOSE 8080
USER nobody
ENTRYPOINT ["./url-shortener"]
