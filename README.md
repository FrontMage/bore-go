# bore-go

A small Go client library for the `bore` TCP tunnel server. This is client-only and mirrors the Rust protocol implementation (null-delimited JSON frames, optional HMAC auth, and on-demand connection proxying).

## Features

- Connect to a `bore` server and request a public port
- Proxy inbound connections to a local TCP service
- Optional secret authentication (HMAC-SHA256)
- Runtime status helpers: connection state, last heartbeat, active proxies

## Installation

Install via:

```bash
go get github.com/FrontMage/bore-go
```

## Usage

```go
package main

import (
    "context"
    "log"

    borego "github.com/FrontMage/bore-go"
)

func main() {
    // Forward local port 8000 to the bore server.
    client, err := borego.NewClient("localhost", 8000, "0.0.0.0", 0, "")
    if err != nil {
        log.Fatalf("failed to start client: %v", err)
    }

    log.Printf("public port assigned: %d", client.RemotePort())

    ctx := context.Background()
    if err := client.Listen(ctx); err != nil {
        log.Fatalf("listen exited: %v", err)
    }
}
```

### With authentication

```go
client, err := borego.NewClient("localhost", 8000, "0.0.0.0", 0, "my-secret")
```

### Status helpers

```go
if client.Connected() {
    log.Println("control connection is alive")
}

if t, ok := client.LastHeartbeat(); ok {
    log.Printf("last heartbeat: %s", t)
}

log.Printf("active proxies: %d", client.ActiveProxies())
```

### Stopping

```go
_ = client.Close()
```

## Related

- Upstream Rust project: https://github.com/ekzhang/bore

## Keywords

`bore`, `bore client`, `tcp tunnel`, `port forwarding`, `reverse proxy`, `nat traversal`, `localtunnel`, `ngrok`

## Notes

- The default control port is `7835` (same as the Rust implementation).
- When `desiredPort` is `0`, the server picks a random available port.
- `Listen` blocks; run it in a goroutine if you need async control.

## License

MIT
