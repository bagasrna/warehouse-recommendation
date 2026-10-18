# Run locally

Run the following commands from the project root. The application includes
its frontend, catalog, and history dataset; no database or external service
is required.

## With Go

Install Go 1.23 or newer, then start the application:

```bash
go run ./cmd/server
```

Open http://localhost:8080. Press Ctrl+C to stop the server.

To use a different port:

```bash
PORT=9090 go run ./cmd/server
```

The optional `DISPATCH_PENALTY_KM_EQ` environment variable configures scoring
sensitivity, for example:

```bash
DISPATCH_PENALTY_KM_EQ=20 go run ./cmd/server
```

## With Docker

Install Docker and start its engine, then build and run:

```bash
docker build -t warehouse-recommendation:local .
docker run --rm --name warehouse-recommendation -p 127.0.0.1:8080:8080 warehouse-recommendation:local
```

Open http://localhost:8080. Press Ctrl+C to stop and remove the container.
The Dockerfile runs the Go tests during the build. Go does not need to be
installed on the host when using Docker.

If port 8080 is already in use, map a different local port:

```bash
docker run --rm --name warehouse-recommendation -p 127.0.0.1:9090:8080 warehouse-recommendation:local
```

Then open http://localhost:9090.

## Verify the application

While the server is running, use another terminal:

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/api/v1/catalog
```

The health endpoint should return `{"status":"ok"}`. Replace `8080` with
your chosen local port if needed.

To run the test suite with a local Go installation:

```bash
go test ./...
```
