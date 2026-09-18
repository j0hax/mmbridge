// Package rcon implements the Source RCON protocol used by Minecraft servers.
// See https://developer.valvesoftware.com/wiki/Source_RCON_Protocol
package rcon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Packet types
	typeAuth         int32 = 3
	typeAuthResponse int32 = 2
	typeCommand      int32 = 2
	typeResponse     int32 = 0

	// Limits
	maxPacketSize = 4096 + 14 // 4096 payload + 14 header/padding
	minPacketSize = 10        // minimum packet: 4 (id) + 4 (type) + 1 (body nul) + 1 (pad nul)
)

var (
	ErrAuthFailed     = errors.New("rcon: authentication failed")
	ErrNotConnected   = errors.New("rcon: not connected")
	ErrResponseTooBig = errors.New("rcon: response packet too large")
)

// Client is a thread-safe RCON client for Minecraft servers.
type Client struct {
	addr     string
	password string
	timeout  time.Duration

	mu   sync.Mutex
	conn net.Conn
	id   int32
}

// NewClient creates a new RCON client. Call Connect() to establish the connection.
func NewClient(addr, password string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		addr:     addr,
		password: password,
		timeout:  timeout,
	}
}

// Connect establishes the TCP connection and authenticates with the server.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return fmt.Errorf("rcon: dial %s: %w", c.addr, err)
	}
	c.conn = conn

	// Authenticate
	id := c.nextID()
	if err := c.writePacket(id, typeAuth, c.password); err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("rcon: auth write: %w", err)
	}

	respID, respType, _, err := c.readPacket()
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("rcon: auth read: %w", err)
	}

	// Minecraft sends type=2 for auth response, with id=-1 on failure.
	if respType == typeAuthResponse && respID == -1 {
		c.conn.Close()
		c.conn = nil
		return ErrAuthFailed
	}
	if respID != id {
		// Some servers send an empty response before the auth response.
		respID, respType, _, err = c.readPacket()
		if err != nil {
			c.conn.Close()
			c.conn = nil
			return fmt.Errorf("rcon: auth read (second): %w", err)
		}
		if respType == typeAuthResponse && respID == -1 {
			c.conn.Close()
			c.conn = nil
			return ErrAuthFailed
		}
	}

	return nil
}

// Execute sends a command and returns the response body.
func (c *Client) Execute(command string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return "", ErrNotConnected
	}

	id := c.nextID()
	if err := c.writePacket(id, typeCommand, command); err != nil {
		return "", fmt.Errorf("rcon: command write: %w", err)
	}

	respID, _, body, err := c.readPacket()
	if err != nil {
		return "", fmt.Errorf("rcon: command read: %w", err)
	}
	if respID != id {
		return "", fmt.Errorf("rcon: response id mismatch: got %d, want %d", respID, id)
	}

	return body, nil
}

// Close closes the underlying TCP connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// IsConnected reports whether there is an active connection.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *Client) nextID() int32 {
	return atomic.AddInt32(&c.id, 1)
}

func (c *Client) writePacket(id, ptype int32, body string) error {
	payload := []byte(body)
	// Packet layout: [size:4][id:4][type:4][body:n][nul:1][nul:1]
	size := int32(4 + 4 + len(payload) + 1 + 1) // id + type + body + 2 nul bytes

	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, size)
	binary.Write(buf, binary.LittleEndian, id)
	binary.Write(buf, binary.LittleEndian, ptype)
	buf.Write(payload)
	buf.WriteByte(0) // body terminator
	buf.WriteByte(0) // packet padding

	c.conn.SetWriteDeadline(time.Now().Add(c.timeout))
	_, err := c.conn.Write(buf.Bytes())
	return err
}

func (c *Client) readPacket() (id, ptype int32, body string, err error) {
	c.conn.SetReadDeadline(time.Now().Add(c.timeout))

	// Read the 4-byte size prefix
	var size int32
	if err = binary.Read(c.conn, binary.LittleEndian, &size); err != nil {
		return 0, 0, "", fmt.Errorf("read size: %w", err)
	}
	if size < int32(minPacketSize) || size > int32(maxPacketSize) {
		return 0, 0, "", fmt.Errorf("invalid packet size %d", size)
	}

	// Read the rest of the packet
	data := make([]byte, size)
	if _, err = io.ReadFull(c.conn, data); err != nil {
		return 0, 0, "", fmt.Errorf("read body: %w", err)
	}

	r := bytes.NewReader(data)
	binary.Read(r, binary.LittleEndian, &id)
	binary.Read(r, binary.LittleEndian, &ptype)

	// Body is everything except the last 2 nul bytes
	bodyBytes := data[8 : len(data)-2]
	body = string(bodyBytes)

	return id, ptype, body, nil
}
