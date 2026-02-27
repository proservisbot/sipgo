package transport

import (
	"log/slog"
	"net"
	"sync"
)

// TODO Connection pool with keeping active connections longer

type ConnectionPool struct {
	// TODO consider sync.Map way with atomic checks to reduce mutex contention
	sync.RWMutex
	m map[string]Connection
}

func NewConnectionPool() *ConnectionPool {
	return &ConnectionPool{
		m: make(map[string]Connection),
	}
}

func (p *ConnectionPool) Add(a string, c Connection) {
	// TODO how about multi connection support for same remote address
	// We can then check ref count

	if c.Ref(0) < 1 {
		c.Ref(1) // Make 1 reference count by default
	}
	p.Lock()
	p.m[a] = c
	p.Unlock()
}

// Getting connection pool increases reference
// Make sure you TryClose after finish
func (p *ConnectionPool) Get(a string) (c Connection) {
	p.RLock()
	c, exists := p.m[a]
	p.RUnlock()
	if !exists {
		return nil
	}
	c.Ref(1)
	// TODO handling more references
	// if c.Ref(1) <= 1 {
	// 	return nil
	// }

	return c
}

// CloseAndDelete closes connection and deletes from pool
func (p *ConnectionPool) CloseAndDelete(c Connection, addr string) {
	p.Lock()
	defer p.Unlock()
	ref, _ := c.TryClose() // Be nice. Saves from double closing
	if ref > 0 {
		if err := c.Close(); err != nil {
			slog.Warn("Closing conection return error", "err", err)
		}
	}
	delete(p.m, addr)
}

// Clear will clear all connection from pool and close them
func (p *ConnectionPool) Clear() {
	p.Lock()
	defer p.Unlock()
	for _, c := range p.m {
		if c.Ref(0) <= 0 {
			continue
		}
		if err := c.Close(); err != nil {
			slog.Warn("Closing conection return error", "err", err)
		}
	}
	// Remove all
	p.m = make(map[string]Connection)
}

// GetByIP returns any connection to the given IP (ignoring port)
// Used for connection reuse when sending requests back to same host
func (p *ConnectionPool) GetByIP(ip string) (c Connection) {
	p.RLock()
	defer p.RUnlock()
	for addr, conn := range p.m {
		// Extract IP from address (format is ip:port)
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// Fallback: try direct match
			host = addr
		}
		if host == ip {
			conn.Ref(1)
			return conn
		}
	}
	return nil
}

// Size returns the number of connections in the pool
func (p *ConnectionPool) Size() int {
	p.RLock()
	l := len(p.m)
	p.RUnlock()
	return l
}
