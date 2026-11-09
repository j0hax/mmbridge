package rcon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func pipeClients(t *testing.T) (*Client, *Client) {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	return &Client{conn: clientConn, timeout: time.Second},
		&Client{conn: serverConn, timeout: time.Second}
}

func TestPacketRoundTrip(t *testing.T) {
	client, server := pipeClients(t)

	serverErr := make(chan error, 1)
	go func() {
		id, ptype, body, err := server.readPacket()
		if err != nil {
			serverErr <- err
			return
		}
		if id != 42 {
			serverErr <- fmt.Errorf("id = %d, want 42", id)
			return
		}
		if ptype != Auth {
			serverErr <- fmt.Errorf("type = %d, want %d", ptype, Auth)
			return
		}
		if body != "secret" {
			serverErr <- fmt.Errorf("body = %q, want %q", body, "secret")
			return
		}
		serverErr <- server.writePacket(id, Response, "ok")
	}()

	if err := client.writePacket(42, Auth, "secret"); err != nil {
		t.Fatalf("writePacket() error = %v", err)
	}

	id, ptype, body, err := client.readPacket()
	if err != nil {
		t.Fatalf("readPacket() error = %v", err)
	}
	if id != 42 || ptype != Response || body != "ok" {
		t.Fatalf("readPacket() = (%d, %d, %q), want (42, %d, %q)", id, ptype, body, Response, "ok")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestExecute(t *testing.T) {
	client, server := pipeClients(t)

	serverErr := make(chan error, 1)
	go func() {
		id, ptype, body, err := server.readPacket()
		if err != nil {
			serverErr <- err
			return
		}
		if ptype != Command {
			serverErr <- fmt.Errorf("type = %d, want %d", ptype, Command)
			return
		}
		if body != "list" {
			serverErr <- fmt.Errorf("body = %q, want %q", body, "list")
			return
		}
		serverErr <- server.writePacket(id, Response, "There are 0 players online")
	}()

	body, err := client.Execute("list")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if body != "There are 0 players online" {
		t.Fatalf("Execute() body = %q, want %q", body, "There are 0 players online")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestExecuteNotConnected(t *testing.T) {
	client := &Client{timeout: time.Second}

	_, err := client.Execute("list")
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Execute() error = %v, want %v", err, ErrNotConnected)
	}
}

func TestExecuteResponseIDMismatch(t *testing.T) {
	client, server := pipeClients(t)

	go func() {
		id, _, _, err := server.readPacket()
		if err != nil {
			return
		}
		_ = server.writePacket(id+1, Response, "wrong id")
	}()

	_, err := client.Execute("list")
	if err == nil {
		t.Fatal("Execute() error = nil, want id mismatch error")
	}
	if !strings.Contains(err.Error(), "response id mismatch") {
		t.Fatalf("Execute() error = %q, want id mismatch", err)
	}
}

func TestReadPacketRejectsInvalidSize(t *testing.T) {
	client, server := pipeClients(t)

	go func() {
		_ = binary.Write(server.conn, binary.LittleEndian, int32(MinPacketSize-1))
	}()

	_, _, _, err := client.readPacket()
	if err == nil {
		t.Fatal("readPacket() error = nil, want invalid size error")
	}
	if !strings.Contains(err.Error(), "invalid packet size") {
		t.Fatalf("readPacket() error = %q, want invalid packet size", err)
	}
}

func TestConnectAuthenticates(t *testing.T) {
	tests := []struct {
		name    string
		replyID int32
		wantErr error
	}{
		{name: "success", replyID: 0, wantErr: nil},
		{name: "failure", replyID: -1, wantErr: ErrAuthFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()

			serverErr := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					serverErr <- err
					return
				}
				defer conn.Close()

				server := &Client{conn: conn, timeout: time.Second}
				id, ptype, body, err := server.readPacket()
				if err != nil {
					serverErr <- err
					return
				}
				if ptype != Auth {
					serverErr <- fmt.Errorf("type = %d, want auth", ptype)
					return
				}
				if body != "password" {
					serverErr <- fmt.Errorf("body = %q, want %q", body, "password")
					return
				}

				replyID := tt.replyID
				if replyID == 0 {
					replyID = id
				}
				serverErr <- server.writePacket(replyID, AuthResponse, "")
			}()

			client := NewClient(ln.Addr().String(), "password", time.Second)
			defer client.Close()

			err = client.Connect()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Connect() error = %v, want %v", err, tt.wantErr)
			}
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
		})
	}
}
