# fastgo

`fastgo` is a small Go-based CLI tool that measures **download and upload internet
speed** using **Fast.com (Netflix Open Connect)** servers.

It works by:

- Fetching the Fast.com app token
- Discovering Netflix speedtest targets
- Measuring real download and upload throughput using parallel connections

The tool is **pure Go**, requires **no external dependencies**, and runs on:

- Linux amd64 (x86\_64)
- macOS amd64 (Intel)
- macOS arm64 (Apple Silicon)
- Linux arm64 (Raspberry Pi 64-bit)
- Linux armv7 (Raspberry Pi 32-bit)

---

## Requirements

- Go **1.25+**
- Internet access (no proxy / TLS interception preferred)

---

## Repository Structure

```text
fastgo/
├── cmd/fastgo/main.go        # CLI entrypoint
├── internal/fastcom/         # Fast.com logic
│   └── fastcom.go
├── go.mod
├── README.md
└── .gitignore
```

## Build

- Linux / macOS (native):

  ```sh
  go fmt ./...
  go mod tidy
  go build -o fastgo ./cmd/fastgo
  ```

- macOS (cross-compile from Linux):

  ```sh
  # Intel
  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 \
    go build -o fastgo-darwin-amd64 ./cmd/fastgo

  # Apple Silicon
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
    go build -o fastgo-darwin-arm64 ./cmd/fastgo
  ```

- ARM (cross-compile):

  ```sh
  # Raspberry Pi (arm64, 64-bit OS)
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -o fastgo-linux-arm64 ./cmd/fastgo

  # Raspberry Pi (armv7, 32-bit OS)
  CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
    go build -o fastgo-linux-armv7 ./cmd/fastgo
  ```

## Run

```sh
./fastgo

# this is equivalent of running
./fastgo -urlcount 5 -parallel 8 -duration 12s -timeout 120s -progress=true -tick=500ms
```

## Flags / Parameters

| Flag        | Default | Description                                    |
| ----------- | ------- | ---------------------------------------------- |
| `-urlcount` | `5`     | Number of Fast.com target URLs to request      |
| `-parallel` | `8`     | Number of parallel workers (download + upload) |
| `-duration` | `12s`   | Duration of each test phase                    |
| `-timeout`  | `120s`  | Overall timeout (token + targets + tests)      |
| `-progress` | `true`  | Show live progress output                      |
| `-tick`     | `500ms` | Progress update interval                       |
