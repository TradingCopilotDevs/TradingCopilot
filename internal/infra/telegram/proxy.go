package telegram

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

type ProxyConfig struct {
	ProxyType string `json:"proxy_type"`
	Addr      string `json:"addr"`
	Port      int    `json:"port"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	RDNS      bool   `json:"rdns"`
}

func ParseProxyURL(raw string) (*ProxyConfig, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	proxyType := ""
	rdns := true
	switch strings.ToLower(parsed.Scheme) {
	case "socks5h":
		proxyType, rdns = "socks5", true
	case "socks5":
		proxyType, rdns = "socks5", false
	case "socks4a":
		proxyType, rdns = "socks4", true
	case "socks4":
		proxyType, rdns = "socks4", false
	case "http":
		proxyType, rdns = "http", true
	default:
		return nil, fmt.Errorf("unsupported Telegram proxy scheme: %s", parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return nil, errors.New("telegram proxy hostname is missing")
	}
	port := parsed.Port()
	defaultPort := "1080"
	if proxyType == "http" {
		defaultPort = "8080"
	}
	if port == "" {
		port = defaultPort
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort <= 0 {
		return nil, fmt.Errorf("telegram proxy port is invalid")
	}
	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	return &ProxyConfig{ProxyType: proxyType, Addr: parsed.Hostname(), Port: parsedPort, Username: username, Password: password, RDNS: rdns}, nil
}

func ProxyDialer(cfg *ProxyConfig) (func(context.Context, string, string) (net.Conn, error), bool) {
	if cfg == nil {
		return nil, false
	}
	proxyAddr := net.JoinHostPort(cfg.Addr, strconv.Itoa(cfg.Port))
	switch cfg.ProxyType {
	case "socks5":
		var authInfo *proxy.Auth
		if cfg.Username != "" || cfg.Password != "" {
			authInfo = &proxy.Auth{User: cfg.Username, Password: cfg.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", proxyAddr, authInfo, proxy.Direct)
		if err != nil {
			return nil, false
		}
		if ctxDialer, ok := dialer.(proxy.ContextDialer); ok {
			return ctxDialer.DialContext, true
		}
		return func(ctx context.Context, network string, addr string) (net.Conn, error) {
			type result struct {
				conn net.Conn
				err  error
			}
			ch := make(chan result, 1)
			go func() {
				conn, err := dialer.Dial(network, addr)
				ch <- result{conn: conn, err: err}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case res := <-ch:
				return res.conn, res.err
			}
		}, true
	case "http":
		return func(ctx context.Context, network string, addr string) (net.Conn, error) {
			var d net.Dialer
			conn, err := d.DialContext(ctx, "tcp", proxyAddr)
			if err != nil {
				return nil, err
			}
			req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: make(http.Header)}
			if cfg.Username != "" || cfg.Password != "" {
				req.SetBasicAuth(cfg.Username, cfg.Password)
			}
			if err := req.Write(conn); err != nil {
				_ = conn.Close()
				return nil, err
			}
			resp, err := http.ReadResponse(bufio.NewReader(conn), req)
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				_ = conn.Close()
				return nil, fmt.Errorf("telegram HTTP proxy CONNECT failed: %s", resp.Status)
			}
			return conn, nil
		}, true
	case "socks4":
		return func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return dialSOCKS4(ctx, proxyAddr, addr, cfg.Username, cfg.RDNS)
		}, true
	default:
		return nil, false
	}
}

func dialSOCKS4(ctx context.Context, proxyAddr string, targetAddr string, userID string, rdns bool) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("telegram SOCKS4 target port is invalid")
	}

	ip := net.ParseIP(host).To4()
	hostForProxy := ""
	if ip == nil {
		if rdns {
			ip = net.IPv4(0, 0, 0, 1).To4()
			hostForProxy = host
		} else {
			addrs, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
			if err != nil {
				return nil, err
			}
			for _, candidate := range addrs {
				if v4 := candidate.To4(); v4 != nil {
					ip = v4
					break
				}
			}
			if ip == nil {
				return nil, fmt.Errorf("telegram SOCKS4 target host has no IPv4 address")
			}
		}
	}
	ip = ip.To4()
	if ip == nil {
		return nil, fmt.Errorf("telegram SOCKS4 target host has no IPv4 address")
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer conn.SetDeadline(time.Time{})
	}
	cancelClose := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-cancelClose:
		}
	}()
	defer close(cancelClose)

	request := make([]byte, 0, 9+len(userID)+len(hostForProxy))
	request = append(request, 0x04, 0x01)
	portBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(portBuf, uint16(port))
	request = append(request, portBuf...)
	request = append(request, ip...)
	request = append(request, userID...)
	request = append(request, 0x00)
	if hostForProxy != "" {
		request = append(request, hostForProxy...)
		request = append(request, 0x00)
	}
	if _, err := conn.Write(request); err != nil {
		_ = conn.Close()
		return nil, err
	}
	response := make([]byte, 8)
	if _, err := io.ReadFull(conn, response); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if response[1] != 0x5a {
		_ = conn.Close()
		return nil, fmt.Errorf("telegram SOCKS4 proxy CONNECT failed: 0x%02x", response[1])
	}
	return conn, nil
}
