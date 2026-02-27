package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	sipgo "github.com/emiago/sipgo/sip"

	"github.com/livekit/sipgo/sip"
)

// TLS transport implementation
type TLSTransport struct {
	*TCPTransport

	// rootPool *x509.CertPool
	tlsConf *tls.Config
}

// NewTLSTransport needs dialTLSConf for creating connections when dialing
func NewTLSTransport(par *sipgo.Parser, dialTLSConf *tls.Config) *TLSTransport {
	tcptrans := NewTCPTransport(par)
	tcptrans.transport = TransportTLS //Override transport
	p := &TLSTransport{
		TCPTransport: tcptrans,
	}

	// p.rootPool = roots
	p.tlsConf = dialTLSConf
	p.log = slog.With("caller", "transport<TLS>")
	return p
}

func (t *TLSTransport) String() string {
	return "transport<TLS>"
}

// CreateConnection creates TLS connection for TCP transport
func (t *TLSTransport) CreateConnection(laddr Addr, host string, raddr Addr, handler sip.MessageHandler) (Connection, error) {
	// raddr, err := net.ResolveTCPAddr("tcp", addr)
	// if err != nil {
	//      return nil, err
	// }

	traddr := &net.TCPAddr{
		IP:   raddr.IP,
		Port: raddr.Port,
	}

	// PATCH: Pass local address if available
	var tladdr *net.TCPAddr
	if laddr.IP != nil {
		tladdr = &net.TCPAddr{
			IP:   laddr.IP,
			Port: laddr.Port,
		}
	}

	return t.createConnection(tladdr, host, traddr, handler)
}

func (t *TLSTransport) createConnection(laddr *net.TCPAddr, host string, raddr *net.TCPAddr, handler sip.MessageHandler) (Connection, error) {
	addr := raddr.String()
	t.log.Debug("Dialing new connection", "raddr", addr, "host", host)

	//TODO does this need to be each config
	// SHould we make copy of rootPool?
	// There is Clone of config

	conf := t.tlsConf.Clone()
	if conf == nil {
		conf = &tls.Config{}
	}
	conf.ServerName = host

	// PATCH: Add timeout and TCP keepalive to prevent AWS NLB flow tracking timeouts
	dialer := &net.Dialer{
		LocalAddr: laddr,
		Timeout:   30 * time.Second,  // Connection timeout
		KeepAlive: 30 * time.Second,  // TCP keepalive interval
	}

	// PATCH: Use context with timeout instead of context.TODO()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%s dial err=%w", t, err)
	}

	// PATCH: Enable TCP keepalive on the connection
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	tconn := tls.Client(conn, conf)

	// PATCH: Perform TLS handshake with timeout
	if err := tconn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s TLS handshake err=%w", t, err)
	}

	c := t.initConnection(tconn, addr, handler)
	return c, nil
}
