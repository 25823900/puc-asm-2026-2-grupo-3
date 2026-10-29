FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /stats-service ./cmd/stats-service

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /stats-service /stats-service
USER 65532:65532
ENV PORT=8000 OTEL_SERVICE_NAME=stats-service
EXPOSE 8000
HEALTHCHECK --interval=10s --timeout=3s CMD ["/stats-service", "healthcheck"]
ENTRYPOINT ["/stats-service"]
