# syntax=docker/dockerfile:1

# lacis-server: the front built by Node, embedded in a static Go binary, on
# distroless as a non-root user. The build stages run on the platform of the
# builder and cross-compile, so a multi-arch build needs no emulation.
#
#   docker build -t lacis-server .
#   docker run --rm -p 8080:8080 -v /etc/lacis:/etc/lacis:ro lacis-server
#
# The image of a release comes from Dockerfile.release instead, built by
# GoReleaser from the binary of the release. Keep both final stages alike.

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY common/go.mod common/go.sum ./common/
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev COMMIT=none DATE=unknown
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w \
        -X github.com/alexandreh2ag/lacis/common/version.Version=${VERSION} \
        -X github.com/alexandreh2ag/lacis/common/version.Commit=${COMMIT} \
        -X github.com/alexandreh2ag/lacis/common/version.Date=${DATE}" \
      -o /out/lacis-server .

# ca-certificates (OIDC provider, PostgreSQL over TLS) and tzdata (the
# schedules run in the local time, TZ) come with the image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/lacis-server /usr/local/bin/lacis-server
USER nonroot:nonroot
EXPOSE 8080
# The configuration is read from /etc/lacis/server.yaml when mounted, and
# from the LACIS_* environment variables.
ENTRYPOINT ["/usr/local/bin/lacis-server"]
CMD ["start", "-c", "/etc/lacis"]
