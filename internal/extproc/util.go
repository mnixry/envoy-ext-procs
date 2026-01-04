package extproc

import (
	"net/http"
	"net/netip"

	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	HeaderEnvoyExternalAddr = "x-envoy-external-address"
)

// GetDownstreamRemoteIP extracts the downstream client IP from ext_proc attributes or headers.
// It first checks the ext_proc attributes for source.address, then falls back to
// the x-envoy-external-address header.
func GetDownstreamRemoteIP(attrs map[string]*structpb.Struct, headers http.Header) (netip.Addr, error) {
	// Try source.address from ext_proc attributes first.
	if attr, ok := attrs["envoy.filters.http.ext_proc"]; ok {
		if field, ok := attr.Fields["source.address"]; ok {
			ip, err := ParseIPFromAddress(field.GetStringValue())
			return oops.Wrap2(ip, err)
		}
	}
	if v := headers.Get(HeaderEnvoyExternalAddr); v != "" {
		ip, err := ParseIPFromAddress(v)
		return oops.Wrap2(ip, err)
	}
	return netip.Addr{}, oops.
		With("attrs", attrs).
		With("headers", headers).
		New("downstream remote IP not found")
}
