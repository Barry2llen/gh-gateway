package sshproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
)

type Server struct {
	ListenAddress   string
	UpstreamAddress string
}

func (s Server) Serve(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.ListenAddress)
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		client, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go proxyConnection(ctx, client, s.UpstreamAddress)
	}
}

func proxyConnection(ctx context.Context, client net.Conn, upstreamAddress string) {
	defer client.Close()
	upstream, err := (&net.Dialer{}).DialContext(ctx, "tcp", upstreamAddress)
	if err != nil {
		return
	}
	defer upstream.Close()

	var wait sync.WaitGroup
	wait.Add(2)
	copyOneWay := func(destination, source net.Conn) {
		defer wait.Done()
		_, _ = io.Copy(destination, source)
		if tcp, ok := destination.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}
	go copyOneWay(upstream, client)
	go copyOneWay(client, upstream)
	wait.Wait()
}
