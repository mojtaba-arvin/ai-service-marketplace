FROM golang:1.23-alpine AS build
WORKDIR /src
COPY apps/api/go.mod ./go.mod
RUN go mod download
COPY apps/api/ ./
RUN go mod tidy \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dispatch-worker ./cmd/dispatch-worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/outbox-worker ./cmd/outbox-worker

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S app \
    && adduser -S -G app -u 10001 app
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/dispatch-worker /app/dispatch-worker
COPY --from=build /out/outbox-worker /app/outbox-worker
COPY --from=build /src/migrations /app/migrations
USER 10001: app
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=20s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/api"]
