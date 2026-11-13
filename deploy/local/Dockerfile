FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY auth ./auth
COPY cmd ./cmd
COPY config ./config
COPY internal ./internal
COPY modules ./modules
COPY pkg ./pkg
ARG VERSION=development
ARG COMMIT=unknown
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/web ./cmd/web && \
    CGO_ENABLED=1 go build -trimpath -o /out/cleanup ./cmd/cleanup

FROM build AS migrationtools
RUN GOBIN=/out go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1

FROM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc AS runtime
ARG VERSION=development
ARG COMMIT=unknown
LABEL org.opencontainers.image.version=$VERSION org.opencontainers.image.revision=$COMMIT
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/web /out/cleanup /usr/local/bin/
WORKDIR /app
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/web"]

FROM runtime AS maintenance
USER root
RUN apt-get update && apt-get install -y --no-install-recommends sqlite3 && rm -rf /var/lib/apt/lists/*
COPY --from=migrationtools /out/migrate /usr/local/bin/
COPY pkg/services/user/sqlite/migrations /migrations/accounts
COPY modules/blog/migrations /migrations/blog
USER 10001:10001
ENTRYPOINT []
