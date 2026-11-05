package providers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agim/lidza/pkg/env"
)

// HostedClient trusts only exact operator-configured backend origins. It never
// changes discovery's public-address policy or follows backend redirects.
func HostedClient(origin string) (*http.Client, error) {
	values, err := env.Values(".")
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, key := range []string{"ZING_ACTIVITYPUB_URL", "ZING_PDS_URL", "ZING_ACTIVITYPUB_AGENT_URL"} {
		if origin != "" && origin == strings.TrimRight(values[key], "/") {
			allowed = true
		}
	}
	u, err := url.Parse(origin)
	if !allowed || err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("hosted backend origin is not configured")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: hostedTransport{origin: origin, base: transport}, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

type hostedTransport struct {
	origin string
	base   http.RoundTripper
}

func (t hostedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme+"://"+req.URL.Host != t.origin || req.URL.User != nil {
		return nil, errors.New("request left its configured hosted origin")
	}
	return t.base.RoundTrip(req)
}
