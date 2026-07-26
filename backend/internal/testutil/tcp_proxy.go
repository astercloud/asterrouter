package testutil

import (
	"errors"
	"io"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"
)

type TCPProxy struct {
	listener net.Listener
	target   string

	mu      sync.Mutex
	enabled bool
	closed  bool
	pairs   map[*tcpProxyPair]struct{}
	wait    sync.WaitGroup
}

type tcpProxyPair struct {
	client   net.Conn
	upstream net.Conn
	once     sync.Once
}

func NewTCPProxy(t testing.TB, targetURL string) (*TCPProxy, string) {
	t.Helper()
	parsed, err := url.Parse(targetURL)
	if err != nil {
		t.Fatalf("parse TCP proxy target URL: %v", err)
	}
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	target := net.JoinHostPort(parsed.Hostname(), port)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for TCP proxy: %v", err)
	}
	proxy := &TCPProxy{listener: listener, target: target, enabled: true, pairs: map[*tcpProxyPair]struct{}{}}
	proxy.wait.Add(1)
	go proxy.accept()
	t.Cleanup(func() {
		if err := proxy.Close(); err != nil {
			t.Errorf("close TCP proxy: %v", err)
		}
	})
	parsed.Host = listener.Addr().String()
	return proxy, parsed.String()
}

func (p *TCPProxy) Disable() {
	p.mu.Lock()
	p.enabled = false
	pairsSnapshot := make([]*tcpProxyPair, 0, len(p.pairs))
	for pair := range p.pairs {
		pairsSnapshot = append(pairsSnapshot, pair)
	}
	p.mu.Unlock()
	for _, pair := range pairsSnapshot {
		pair.close()
	}
}

func (p *TCPProxy) Enable() {
	p.mu.Lock()
	if !p.closed {
		p.enabled = true
	}
	p.mu.Unlock()
}

func (p *TCPProxy) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.enabled = false
	pairsSnapshot := make([]*tcpProxyPair, 0, len(p.pairs))
	for pair := range p.pairs {
		pairsSnapshot = append(pairsSnapshot, pair)
	}
	p.mu.Unlock()
	err := p.listener.Close()
	for _, pair := range pairsSnapshot {
		pair.close()
	}
	p.wait.Wait()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (p *TCPProxy) accept() {
	defer p.wait.Done()
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.wait.Add(1)
		go p.relay(client)
	}
}

func (p *TCPProxy) relay(client net.Conn) {
	defer p.wait.Done()
	p.mu.Lock()
	enabled := p.enabled && !p.closed
	p.mu.Unlock()
	if !enabled {
		_ = client.Close()
		return
	}
	upstream, err := net.DialTimeout("tcp", p.target, 2*time.Second)
	if err != nil {
		_ = client.Close()
		return
	}
	pair := &tcpProxyPair{client: client, upstream: upstream}
	p.mu.Lock()
	if !p.enabled || p.closed {
		p.mu.Unlock()
		pair.close()
		return
	}
	p.pairs[pair] = struct{}{}
	p.mu.Unlock()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
	pair.close()
	<-done
	p.mu.Lock()
	delete(p.pairs, pair)
	p.mu.Unlock()
}

func (p *tcpProxyPair) close() {
	p.once.Do(func() {
		_ = p.client.Close()
		_ = p.upstream.Close()
	})
}
