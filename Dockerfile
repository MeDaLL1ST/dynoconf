# syntax=docker/dockerfile:1

# --- Stage 1: build the frontend (arch-independent, runs on the build host) ---
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm install
COPY web/ ./
RUN npm run build

# --- Stage 2: build the Go binary with the embedded frontend ---
# Runs on the build host's arch and cross-compiles to the target via GOOS/GOARCH,
# so multi-arch builds don't need QEMU emulation for the Go toolchain.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Replace the placeholder dist with the freshly built frontend, then embed it.
RUN rm -rf web/dist
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /dynoconf ./cmd/server

# --- Stage 3: minimal runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /dynoconf /dynoconf
# HTTP_ADDR (UI/REST) and GRPC_ADDR (apps) default to :8080 / :9090.
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/dynoconf"]
