// Package httptrace provides OpenTelemetry instrumentation wrappers for http Clients and Transports.
package httptrace

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// WrapClient clones *http.Client and installs OpenTelemetry transport instrumentation.
func WrapClient(c *http.Client) *http.Client {
	if c == nil {
		c = http.DefaultClient
	}
	clone := *c
	tr := c.Transport
	if tr == nil {
		tr = http.DefaultTransport
	}
	clone.Transport = otelhttp.NewTransport(tr,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
	return &clone
}

// WrapTransport constructs OpenTelemetry http.RoundTripper around given transport.
func WrapTransport(tr http.RoundTripper) http.RoundTripper {
	if tr == nil {
		tr = http.DefaultTransport
	}
	return otelhttp.NewTransport(tr)
}
