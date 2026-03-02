# SIP Connection Reuse for SBC Scenarios

## Problem

When using Session Border Controllers (SBCs) that connect from high ephemeral ports, REFER requests would fail with:
```
transport<TLS> dial err=dial tcp <nil>->10.171.228.95:5061: connect: connection timed out
```

This occurred because:
1. Inbound call arrives from SBC on high port (e.g., `10.171.228.95:20561`)
2. Connection is stored in pool keyed by full address `IP:port`
3. REFER to `avayacloud.com:5061` resolves to same IP but different port (`10.171.228.95:5061`)
4. Pool lookup for exact match `IP:5061` fails
5. Code tries to create new outbound connection which times out

## Solution

Added IP-based connection reuse fallback. When exact address match fails, the code now searches for any connection to the same IP regardless of the remote port.

## Changes

### transport/connection_pool.go:82-100
Added `GetByIP()` method to search connection pool by IP address, ignoring port:
```go
// GetByIP returns any connection to the given IP (ignoring port)
// Used for connection reuse when sending requests back to same host
func (p *ConnectionPool) GetByIP(ip string) (c Connection) {
    p.RLock()
    defer p.RUnlock()
    for addr, conn := range p.m {
        // Extract IP from address (format is ip:port)
        host, _, err := net.SplitHostPort(addr)
        if err != nil {
            host = addr
        }
        if host == ip {
            conn.Ref(1)
            return conn
        }
    }
    return nil
}
```

### transport/layer.go:364-382
Added IP-based fallback lookup in `ClientRequestConnection()`:
```go
// Try to find any connection to the same IP (for SBC connection reuse)
remoteHost, _, _ := sip.ParseAddr(addr)
l.log.Debug("Connection pool lookup by IP", "host", remoteHost)
c, _ = transport.GetConnectionByIP(remoteHost)
if c != nil {
    l.log.Debug("Found connection by IP", "host", remoteHost)
    laddr := c.LocalAddr()
    network := laddr.Network()
    laddrStr := laddr.String()
    host, port, err := sip.ParseAddr(laddrStr)
    if err != nil {
        return nil, fmt.Errorf("fail to parse local connection address network=%s addr=%s: %w", network, laddrStr, err)
    }
    if viaHop.Host == "" {
        viaHop.Host = host
    }
    viaHop.Port = port
    return c, nil
}
l.log.Debug("Active connection not found", "addr", addr)
```

### transport/transport.go:39
Added `GetConnectionByIP()` to Transport interface:
```go
// GetConnectionByIP gets any connection to the given IP (ignoring port)
GetConnectionByIP(ip string) (Connection, error)
```

### transport/tcp.go:83-87
Implemented `GetConnectionByIP()` for TCP transport:
```go
func (t *TCPTransport) GetConnectionByIP(ip string) (Connection, error) {
    t.log.Debug("Getting connection by IP", "ip", ip)
    c := t.pool.GetByIP(ip)
    return c, nil
}
```

### transport/udp.go:108-121
Implemented `GetConnectionByIP()` for UDP transport:
```go
func (t *UDPTransport) GetConnectionByIP(ip string) (Connection, error) {
    t.log.Debug("Getting connection by IP", "ip", ip)
    c := t.pool.GetByIP(ip)
    if c != nil {
        return c, nil
    }
    // Fallback to listener if no specific connection found
    t.mu.Lock()
    defer t.mu.Unlock()
    if len(t.listeners) > 0 {
        return t.listeners[0], nil
    }
    return nil, nil
}
```

### transport/ws.go:213-217
Implemented `GetConnectionByIP()` for WebSocket transport:
```go
func (t *WSTransport) GetConnectionByIP(ip string) (Connection, error) {
    t.log.Debug("Getting connection by IP", "ip", ip)
    c := t.pool.GetByIP(ip)
    return c, nil
}
```

### TLS and WSS Transports
`TLSTransport` and `WSSTransport` inherit `GetConnectionByIP()` from their embedded `TCPTransport` and `WSTransport` respectively.

## Flow

1. Client sends REFER to `sip:user@avayacloud.com:5061`
2. DNS resolves `avayacloud.com` to `10.171.228.95`
3. Code looks for exact connection to `10.171.228.95:5061` → Not found
4. Code falls back to IP lookup for `10.171.228.95` → Found existing connection from SBC (port 20561)
5. Existing connection is reused for REFER request
6. Via header is updated with the connection's local address

## Testing

Look for these log messages to verify the fix:
- `"Connection pool lookup" addr=X found=false` - Exact match failed
- `"Connection pool lookup by IP" host=X` - Trying IP fallback
- `"Found connection by IP" host=X` - Successfully found connection by IP

## Related Changes

Also included in this fork:
- TCP keepalive and TLS handshake improvements (`tls.go`)
- Debug logging for connection pool and dial operations
