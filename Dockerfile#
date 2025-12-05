# syntax=docker/dockerfile:1.7

############################
# Build stage
############################
FROM golang:1.23.4 AS build
WORKDIR /app

# Speed + reliability for module fetches
ENV GOPROXY=https://proxy.golang.org,direct
# Allow go to fetch the exact toolchain version if needed
ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath -buildvcs=false -o /server .

    
############################
# Run stage (Cloud Run)
############################
FROM gcr.io/distroless/base-debian12
ENV PORT=8080
WORKDIR /srv
COPY --from=build /server /srv/server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/srv/server"]
