package vdoc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

func newAIHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid ai provider host", ErrInvalidArgument)
		}
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if err := validateAIProviderResolvedAddrs(host, addrs); err != nil {
			return nil, err
		}
		var lastErr error
		for _, addr := range addrs {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: rejectAIProviderRedirect}
}

func rejectAIProviderRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func normalizeAIProviderBaseURL(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", ErrInvalidArgument
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return "", fmt.Errorf("%w: invalid ai provider base_url", ErrInvalidArgument)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: unsafe ai provider base_url", ErrInvalidArgument)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || strings.ContainsAny(host, " \t\r\n%") || isUnsafeAIProviderHost(host) {
		return "", fmt.Errorf("%w: unsafe ai provider base_url", ErrInvalidArgument)
	}
	port := parsed.Port()
	parsed.Scheme = "https"
	parsed.Host = host
	if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	}
	if port != "" {
		parsed.Host += ":" + port
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func isUnsafeAIProviderHost(host string) bool {
	domain := strings.TrimSuffix(host, ".")
	if domain == "localhost" || strings.HasSuffix(domain, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(domain)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() || isMetadataIP(addr)
}

func validateAIProviderResolvedAddrs(host string, addrs []netip.Addr) error {
	if len(addrs) == 0 {
		return fmt.Errorf("%w: ai provider host %q has no addresses", ErrInvalidArgument, host)
	}
	for _, addr := range addrs {
		if isUnsafeAIProviderAddr(addr) {
			return fmt.Errorf("%w: ai provider host %q resolved to unsafe address", ErrInvalidArgument, host)
		}
	}
	return nil
}

func isUnsafeAIProviderAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() || isMetadataIP(addr)
}

func isMetadataIP(addr netip.Addr) bool {
	switch addr.String() {
	case "169.254.169.254", "169.254.169.253", "100.100.100.200", "168.63.129.16", "fd00:ec2::254":
		return true
	default:
		return false
	}
}
