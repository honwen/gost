# AGENTS.md

Guidance for AI agents (and humans) working in this repository.

## Overview

GOST (GO Simple Tunnel) v2 — a multi-protocol tunnel/proxy written in Go:
HTTP/HTTPS/HTTP2/SOCKS4(A)/SOCKS5/Shadowsocks/SNI proxy servers and clients,
port forwarding, TCP/UDP transparent proxy, SSH/QUIC/KCP/WS/WSS/TLS tunnels.

This repository (`ginuerzh/gost`) is in **maintenance mode**: no new features.
It has been superseded by [go-gost/gost](https://github.com/go-gost/gost) (v3).

## Module

| Field | Value |
|-------|-------|
| Module path | `github.com/ginuerzh/gost` |
| Package name | `gost` (root package is the library) |
| Go version | `1.26` (see `go.mod`, currently `go 1.26.9`) |
| License | MIT |

## Layout

| Path | Purpose |
|------|---------|
| `*.go` (root) | the library; one file per protocol/transport/feature |
| `cmd/gost/` | the CLI (`main.go` flags/config, `route.go` URL-to-node wiring) |
| `examples/` | standalone runnable programs, **one `main` per directory** |
| `.config/` | sample config files used by `-C` |
| `.goreleaser.yaml`, `Makefile` | release config and local cross-builds |

## Build & test

```bash
go build ./cmd/gost        # build the CLI; writes ./gost (git-ignored)
go build ./...             # build everything, including examples
go vet ./...               # static analysis; also type-checks all _test.go files
go test .                  # library test suite (see caveats below)
go test -count=1 -run TestSOCKS5 -v .
gofmt -l .                 # formatting check
make linux-amd64           # cross-compile into bin/ via the Makefile
goreleaser check           # validate the release config
```

### Test suite caveats

`go test .` is **not green on a clean checkout**. As of 2026-10 there are 17
pre-existing failures (they reproduce on the pre-dependency-upgrade HEAD):

| Test | Cause |
|------|-------|
| `TestDNSResolver` | needs to reach `1.1.1.1:853` (DoT); fails without internet |
| `TestShadowTCP`, `TestShadowUDP`, `TestSSOver*` | shadowsocks AEAD cases with a password (`#70 #71 #72 #78 #83 #85 #86 #87 #93 #11`): mismatched passwords "succeed" and matching ones fail; suspect `initShadowCipher` in `ss.go` |
| `TestH2ForwardTunnel`, `TestH2CForwardTunnel` | `Connect "https:": stream error: ID 1; PROTOCOL_ERROR` |

Because of this, CI (`.github/workflows/release.yml`) only runs GoReleaser on
tag pushes — it does not run the tests either. When you touch code and want to
know whether you broke something, always build and vet, and diff the test
results against a clean baseline instead of trusting a single run:

```bash
git worktree add /tmp/gost-baseline HEAD
(cd /tmp/gost-baseline && go test -count=1 . 2>&1 | grep '^--- FAIL' | sort) > /tmp/base.txt
go test -count=1 . 2>&1 | grep '^--- FAIL' | sort | diff /tmp/base.txt -
```

Many tests use 1s deadlines and local ports; they can be flaky under load.

## Architecture

Requests flow through a fixed pipeline:

```
Listener ──Accept──▶ Server ──Handle──▶ Handler ──Dial──▶ Chain ──▶ Connector (+ Transporter)
```

| Concept | Role | Key files |
|---------|------|-----------|
| `Listener` | accepts `net.Conn`s (TCP/UDP/TLS/WS/KCP/QUIC/SSH/H2/…) | `tcp.go`, `udp.go`, `tls.go`, `ws.go`, `kcp.go`, `quic.go`, `ssh.go`, `http2.go` |
| `Server` | accept loop, hands conns to a `Handler` | `server.go` |
| `Handler` | serves one conn: authenticates, checks whitelist/bypass, dials the target, then `transport()` (bidirectional copy) | `handler.go`, `socks.go`, `http.go`, `forward.go`, `relay.go`, `ss.go`, `sni.go` |
| `Chain` | ordered list of `Node`s a connection is dialed through | `chain.go`, `node.go` |
| `Connector` | protocol-specific dial (SOCKS5, HTTP, SS, relay, SSH forward…) | `client.go`, `socks.go`, `http.go`, `ss.go`, `relay.go` |
| `Transporter` | transport handshake/stream multiplexing under a connector | `tls.go`, `ws.go`, `kcp.go`, `quic.go`, `http2.go`, `mux.go` |

The CLI maps URLs (`scheme[+transport]://user:pass@host:port/remote?opts`) to
those objects in `cmd/gost/route.go`; the library exposes the same pieces as
public constructors (`SOCKS5Handler`, `TCPDirectForwardHandler`, `NewChain`, …).

## Gotchas

1. **`Server.Serve` does not call `Handler.Init()`.** `SOCKS5Handler`,
   `HTTPHandler`, `ShadowHandler`, `ShadowUDPHandler`, `SSHForwardHandler`, etc.
   call `Init` inside their constructors, but the forward handlers
   (`TCPDirectForwardHandler`, `UDPDirectForwardHandler`,
   `TCPRemoteForwardHandler`, `UDPRemoteForwardHandler`) do **not** — call
   `h.Init()` yourself, otherwise their node group stays empty and they
   silently dial the wrong address (`raddr` is only parsed by `Init`).
2. **A hand-built `gost.Node` needs `Host` and `DialOptions: []gost.DialOption{gost.HostDialOption(...)}`.**
   The CLI fills these from the URL; without them the h2/http2 connectors send
   a `CONNECT` request with an empty host (server answers `PROTOCOL_ERROR`).
3. **`gost.DefaultTLSConfig` is nil unless the program sets it** (the CLI
   generates a certificate in `main`). Components fall back to it when no
   explicit config is given; `SSHTunnelListener` dereferences it and panics if
   it is nil.
4. **The default logger is a no-op** (`github.com/go-log/log`). Call
   `gost.SetLogger(&gost.LogLogger{})` in any program that should log;
   `gost.Debug = true` only adds detail, it does not enable output.
5. **Writing a SOCKS5 UDP datagram to a datagram socket must be one `Write`.**
   `gosocks5.UDPDatagram.WriteTo` writes the header and the payload with
   separate `Write` calls, which produces several UDP packets; assemble it in a
   `bytes.Buffer` first (see `socks5UDPConn.WriteTo`).
6. The relay wire protocol encodes the UDP flag in `Request.Cmd`
   (`relay.FUDP = 0x80`), the byte that older library versions called `Flags` —
   it is byte-compatible with pre-v0.7 peers.
7. **`linux/s390x` is deliberately not built.** With Go 1.26 the s390x backend
   compiles `binary.LittleEndian.PutUint32` inside `github.com/aead/chacha20`
   (a dependency of the legacy `shadowsocks-go` library) into an illegal
   `MOVWBR`, so the target is dropped from the `Makefile` and
   `.goreleaser.yaml`. Re-adding it needs a fixed toolchain or dropping that
   dependency on s390x.

## Dependencies worth knowing

| Module | Used for | Version-specific notes |
|--------|----------|------------------------|
| `github.com/go-gost/gosocks5` | SOCKS5 conn/request/addr codec | v0.5: `Selector.OnSelected` returns a client ID, `ReadUDPDatagram` was removed, `UDPDatagram.Write` → `WriteTo` |
| `github.com/go-gost/gosocks4` | SOCKS4(A) | v0.1: `Addr.Type int`, `Ver4` |
| `github.com/go-gost/relay` | relay protocol | v0.7: `Request.Cmd` (not `Flags`), `Feature` interface with `Encode`/`Decode`, `AddrType` |
| `github.com/quic-go/quic-go` | QUIC transport | v0.63: `*quic.Conn`, `*quic.Stream` (structs, not interfaces), `quic.Version` |
| `github.com/gobwas/glob` | domain/bypass wildcards | v1.0: `*glob.Pattern` |
| `github.com/xtaci/kcp-go/v5`, `xtaci/smux` | KCP transport, multiplexing | |
| `github.com/shadowsocks/go-shadowsocks2`, `shadowsocks-go` | Shadowsocks ciphers | cipher+password come from the `Users` handler option (`initShadowCipher`) |
| `github.com/go-gost/tls-dissector` | SNI parsing | |
| `git.torproject.org/.../goptlib`, `gitlab.com/yawning/obfs4.git` | obfs4 transport | |
| `github.com/miekg/dns` | DNS resolver / `dns` proxy | |

## Conventions

- Tests live next to the code as `*_test.go`, table-driven with
  `t.Run(fmt.Sprintf("#%d", i), ...)` and small round-trip helpers in
  `common_test.go`; add a test when fixing a protocol bug.
- Examples are separate programs: one `main` per directory (`examples/<feature>/<role>/main.go`).
  Keep `go build ./...` green for them.
- Comments and commit messages are in English, short and lower-case
  (`fix socks5 udp relay`, `update README`).
- Avoid new dependencies: the point of this repo is a self-contained binary.
