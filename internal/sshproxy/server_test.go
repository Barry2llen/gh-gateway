package sshproxy

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestProxyConnectionCopiesBothDirections(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	upstreamDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			upstreamDone <- err
			return
		}
		defer connection.Close()
		buffer := make([]byte, 4)
		if _, err := io.ReadFull(connection, buffer); err != nil {
			upstreamDone <- err
			return
		}
		if string(buffer) != "ping" {
			upstreamDone <- io.ErrUnexpectedEOF
			return
		}
		_, err = connection.Write([]byte("pong"))
		if tcp, ok := connection.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		upstreamDone <- err
	}()

	client, proxySide := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go proxyConnection(ctx, proxySide, listener.Addr().String())
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "pong" {
		t.Fatalf("response = %q", response)
	}
	_ = client.Close()
	if err := <-upstreamDone; err != nil {
		t.Fatal(err)
	}
}
