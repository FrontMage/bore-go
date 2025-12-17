package borego

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Client maintains a control connection to a bore server and proxies incoming connections.
type Client struct {
	conn       *Delimited
	to         string
	localHost  string
	localPort  uint16
	remotePort uint16
	auth       *Authenticator

	connected     atomic.Bool
	activeProxies atomic.Int64
	lastHeartbeat atomic.Int64
	closeOnce     sync.Once
}

// NewClient connects to the remote server and performs the initial handshake.
func NewClient(localHost string, localPort uint16, to string, desiredPort uint16, secret string) (*Client, error) {
	conn, err := connectWithTimeout(to, ControlPort)
	if err != nil {
		return nil, err
	}
	framed := NewDelimited(conn)

	var auth *Authenticator
	if secret != "" {
		auth = NewAuthenticator(secret)
		if err := auth.ClientHandshake(framed); err != nil {
			conn.Close()
			return nil, err
		}
	}

	if err := framed.SendJSON(encodeHello(desiredPort)); err != nil {
		conn.Close()
		return nil, err
	}

	msg, ok, err := framed.RecvServer(true)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !ok {
		conn.Close()
		return nil, ErrUnexpectedEOF
	}

	switch msg.Kind {
	case ServerHello:
		log.Printf("connected to server, remote port %d", msg.Port)
	case ServerError:
		conn.Close()
		return nil, fmt.Errorf("server error: %s", msg.ErrorText)
	case ServerChallenge:
		conn.Close()
		return nil, fmt.Errorf("server requires authentication but no secret was provided")
	default:
		conn.Close()
		return nil, fmt.Errorf("unexpected initial message: %v", msg.Kind)
	}

	client := &Client{
		conn:       framed,
		to:         to,
		localHost:  localHost,
		localPort:  localPort,
		remotePort: msg.Port,
		auth:       auth,
	}
	client.connected.Store(true)
	return client, nil
}

// RemotePort returns the public port assigned by the server.
func (c *Client) RemotePort() uint16 {
	return c.remotePort
}

// Connected reports whether the control connection is currently active.
func (c *Client) Connected() bool {
	return c.connected.Load()
}

// ActiveProxies returns the number of currently active proxy connections.
func (c *Client) ActiveProxies() int64 {
	return c.activeProxies.Load()
}

// LastHeartbeat returns the last heartbeat time and whether one has been observed.
func (c *Client) LastHeartbeat() (time.Time, bool) {
	ns := c.lastHeartbeat.Load()
	if ns == 0 {
		return time.Time{}, false
	}
	return time.Unix(0, ns), true
}

// Close shuts down the control connection.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.connected.Store(false)
		if c.conn != nil {
			err = c.conn.Close()
		}
	})
	return err
}

// Listen waits for new connection notifications from the server and proxies them.
func (c *Client) Listen(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer c.connected.Store(false)

	go func() {
		<-ctx.Done()
		_ = c.Close()
	}()

	c.connected.Store(true)
	for {
		msg, ok, err := c.conn.RecvServer(false)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		switch msg.Kind {
		case ServerHeartbeat:
			c.lastHeartbeat.Store(time.Now().UnixNano())
		case ServerConnection:
			id := msg.ID
			go func() {
				if err := c.handleConnection(id); err != nil {
					log.Printf("proxy for %s ended with error: %v", id, err)
				}
			}()
		case ServerHello, ServerChallenge:
			log.Printf("unexpected control message: %v", msg.Kind)
		case ServerError:
			return fmt.Errorf("server error: %s", msg.ErrorText)
		default:
			log.Printf("unknown server message: %+v", msg)
		}
	}
}

func (c *Client) handleConnection(id uuid.UUID) error {
	c.activeProxies.Add(1)
	defer c.activeProxies.Add(-1)

	remoteConn, err := connectWithTimeout(c.to, ControlPort)
	if err != nil {
		return fmt.Errorf("connect to server for proxy: %w", err)
	}
	framed := NewDelimited(remoteConn)
	if c.auth != nil {
		if err := c.auth.ClientHandshake(framed); err != nil {
			framed.Close()
			return err
		}
	}
	if err := framed.SendJSON(encodeAccept(id)); err != nil {
		framed.Close()
		return err
	}

	// Clear deadlines before handing the socket to io.Copy.
	_ = remoteConn.SetDeadline(time.Time{})

	buffered, err := framed.BufferedData()
	if err != nil {
		framed.Close()
		return err
	}

	localConn, err := connectWithTimeout(c.localHost, c.localPort)
	if err != nil {
		framed.Close()
		return fmt.Errorf("connect to local service: %w", err)
	}

	if len(buffered) > 0 {
		if _, err := localConn.Write(buffered); err != nil {
			localConn.Close()
			framed.Close()
			return fmt.Errorf("write buffered data to local: %w", err)
		}
	}

	return proxy(localConn, framed.RawConn())
}

func connectWithTimeout(host string, port uint16) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(int(port)))
	conn, err := net.DialTimeout("tcp", addr, NetworkTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}
