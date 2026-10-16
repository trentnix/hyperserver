FROM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY auth ./auth
COPY cmd ./cmd
COPY config ./config
COPY modules ./modules
COPY pkg ./pkg
ARG VERSION=development
ARG COMMIT=unknown
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/web ./cmd/web && \
    CGO_ENABLED=1 go build -trimpath -o /out/cleanup ./cmd/cleanup

FROM build AS migrationtools
RUN GOBIN=/out go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587 AS runtime
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
COPY modules/site/database/migrations /migrations/contacts
USER 10001:10001
ENTRYPOINT []
