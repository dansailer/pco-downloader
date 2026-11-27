package pco

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const maxDownload = 512 << 20

// newHTTPClient refuses private, loopback, and link-local destinations except
// when the configured API host is itself local (tests). Cleartext is allowed
// only for that same local host. TLS 1.2 is the minimum for every other host.
func (c *Client) newHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return c.dial(ctx, dialer, network, addr)
			},
			ResponseHeaderTimeout: 30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConns:          8,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			if err := c.validateFetchURL(req.URL.String()); err != nil {
				return err
			}
			// Replacing the default redirect policy means we must drop the
			// credential ourselves when the host changes.
			if !apiAuthedFile(c.base(), req.URL.String()) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

func (c *Client) dial(ctx context.Context, d *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	// A proxy the user configured is dialed as named. Every other connection
	// uses an address that already passed allowIP, so a DNS change between
	// the check and the connect cannot retarget it.
	if proxyHost(host) {
		return d.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
	ips, err := lookupIPs(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error
	for _, ip := range ips {
		if !c.allowIP(host, ip) {
			last = fmt.Errorf("refusing connection to %s", host)
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no usable address for %s", host)
	}
	return nil, last
}

func proxyHost(host string) bool {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if strings.EqualFold(u.Hostname(), host) {
			return true
		}
	}
	return false
}

func lookupIPs(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.IP)
	}
	return out, nil
}

func (c *Client) validateFetchURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("attachment URL is not a file link")
	}
	if u.User != nil {
		return errors.New("refusing a file URL that contains credentials")
	}
	host := u.Hostname()
	if u.Scheme == "http" && !c.localAPIHost(host) {
		return errors.New("refusing a non-HTTPS file URL")
	}
	if ip := net.ParseIP(host); ip != nil && !c.allowIP(host, ip) {
		return fmt.Errorf("refusing connection to %s", host)
	}
	if isBlockedName(host) && !c.localAPIHost(host) {
		return fmt.Errorf("refusing connection to %s", host)
	}
	return nil
}

// allowIP reports whether ip may be dialed for host. Public addresses are
// allowed. Private and link-local addresses are allowed only when host is the
// configured API host and that host is itself local.
func (c *Client) allowIP(host string, ip net.IP) bool {
	if ip == nil || !riskyIP(ip) {
		return ip != nil
	}
	return c.localAPIHost(host)
}

func (c *Client) localAPIHost(host string) bool {
	base := hostname(c.base())
	if base == "" || !strings.EqualFold(host, base) {
		return false
	}
	return isLocalName(base)
}

func hostname(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// sameEndpoint reports whether a and b are the same scheme, host, and port.
// An omitted port matches the scheme default, so :443 is the same as https.
func sameEndpoint(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	if !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	pa, pb := endpointPort(a), endpointPort(b)
	return pa != "" && pa == pb
}

func endpointPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

func isLocalName(host string) bool {
	h := strings.ToLower(host)
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && riskyIP(ip)
}

func isBlockedName(host string) bool {
	h := strings.ToLower(host)
	switch h {
	case "localhost", "metadata", "metadata.google.internal", "metadata.google.com":
		return true
	}
	return strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal")
}

func riskyIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	// 0.0.0.0/8 and carrier-grade NAT 100.64.0.0/10.
	if v4[0] == 0 || (v4[0] == 100 && v4[1]&0xc0 == 0x40) {
		return true
	}
	return false
}

func stripURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid url"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
