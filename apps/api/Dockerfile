FROM golang:1.25-alpine AS build
WORKDIR /src
COPY apps/api/go.mod ./go.mod
RUN go mod download
COPY apps/api/ ./
RUN go mod tidy \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dispatch-worker ./cmd/dispatch-worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/outbox-worker ./cmd/outbox-worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/analytics-worker ./cmd/analytics-worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/analytics-rebuild ./cmd/analytics-rebuild \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/operator-permissions-bootstrap ./cmd/operator-permissions-bootstrap

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S app \
    && adduser -S -G app -u 10001 app
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/migrate /app/migrate
COPY --from=build /out/dispatch-worker /app/dispatch-worker
COPY --from=build /out/outbox-worker /app/outbox-worker
COPY --from=build /out/analytics-worker /app/analytics-worker
COPY --from=build /out/analytics-rebuild /app/analytics-rebuild
COPY --from=build /out/operator-permissions-bootstrap /app/operator-permissions-bootstrap
COPY --from=build /src/migrations /app/migrations
USER app:app
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=20s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/readyz || exit 1
ENTRYPOINT ["/app/api"]
