// Package providers contains bounded network clients. No provider credentials
// or untrusted response bodies are included in errors or logs.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
)

func PublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "64:ff9b::/96", "2002::/16", "2001::/32"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}
func Server(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Port() != "" && u.Port() != "443") {
		return "", router.Errorf(422, "Use an HTTPS server hostname")
	}
	return "https://" + u.Host, nil
}
func Client() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, errors.New("server lookup failed")
		}
		if len(ips) == 0 {
			return nil, errors.New("server has no address")
		}
		for _, ip := range ips {
			if !PublicIP(ip) {
				return nil, router.Errorf(422, "Server must resolve to a public address")
			}
		}
		var last error
		for _, ip := range ips {
			c, e := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	transport.TLSHandshakeTimeout = 8 * time.Second
	transport.ResponseHeaderTimeout = 15 * time.Second
	return &http.Client{Transport: transport, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Session is server-only. Never return or log this type.
type Session struct {
	Auth                                              func(*http.Request) (*http.Response, error)
	Proxy                                             string
	Network, Server, Subject, Handle, Access, Refresh string
	ExpiresAt                                         *time.Time
	Client                                            *http.Client
}

func (s Session) Do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	content := "application/json"
	switch v := body.(type) {
	case url.Values:
		reader = strings.NewReader(v.Encode())
		content = "application/x-www-form-urlencoded"
	case []byte:
		reader = bytes.NewReader(v)
	default:
		if body != nil {
			b, e := json.Marshal(body)
			if e != nil {
				return e
			}
			reader = bytes.NewReader(b)
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, s.Server+path, reader)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", content)
	req.Header.Set("Accept", "application/json")
	if s.Proxy != "" {
		req.Header.Set("atproto-proxy", s.Proxy)
	}
	if s.Access != "" {
		req.Header.Set("Authorization", "Bearer "+s.Access)
	}
	resp, e := s.Request(req)
	if e != nil {
		var safe *router.HTTPError
		if errors.As(e, &safe) {
			return safe
		}
		return errors.New("provider request could not be confirmed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProviderError(resp.StatusCode)
	}
	if out == nil {
		_, e = io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20))
		return e
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}
func ProviderError(status int) error {
	switch status {
	case 401, 403:
		return router.ErrorCode(409, "reconnect_required", "Reconnect this account or grant the required permission")
	case 429:
		return router.ErrorCode(429, "rate_limited", "Network rate limit reached. Try again later.")
	case 404:
		return router.NotFound("Network resource")
	default:
		if status >= 500 {
			return router.ErrorCode(502, "provider_unavailable", "The network is temporarily unavailable")
		}
		return router.ErrorCode(422, "provider_rejected", "The network rejected this operation")
	}
}
func Unsupported() error {
	return router.ErrorCode(422, "unsupported", "This network does not expose this operation through its approved API")
}

func (s Session) Request(req *http.Request) (*http.Response, error) {
	if s.Auth != nil {
		return s.Auth(req)
	}
	c := s.Client
	if c == nil {
		c = Client()
		if env.Mode() == "test" {
			if rt, ok := lidza.Optional[http.RoundTripper](req.Context()); ok {
				c.Transport = rt
			}
		}
	}
	return c.Do(req)
}
