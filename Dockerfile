# Official Go builder image (pinned to a stable patch release).
FROM golang:1.27.2-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY . .
RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/warehouse-recommendation ./cmd/server

# No shell, package manager, compiler, database, or static-file server needed.
FROM scratch
COPY --from=builder /out/warehouse-recommendation /warehouse-recommendation
USER 65532:65532
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/warehouse-recommendation"]
