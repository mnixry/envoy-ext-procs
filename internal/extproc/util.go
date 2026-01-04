package extproc

import (
	"net/http"
	"net/netip"
	"strings"

	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	HeaderEnvoyExternalAddr = "x-envoy-external-address"
)

func ParseIPFromAddress(addr string) (netip.Addr, error) {
	ip, errParse := netip.ParseAddr(strings.Trim(addr, "[]"))
	if errParse == nil {
		return ip, nil
	}
	ap, errParseAddrPort := netip.ParseAddrPort(addr)
	if errParseAddrPort == nil {
		return ap.Addr(), nil
	}
	return netip.Addr{}, oops.
		In("extproc").
		Code("PARSE_IP_FROM_ADDRESS_FAILED").
		With("addr", addr).
		Join(errParse, errParseAddrPort)
}

func GetDownstreamRemoteIP(attrs map[string]*structpb.Struct, headers http.Header) (netip.Addr, error) {
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

func FirstNonEmpty[T comparable](values ...T) T {
	var empty T
	for _, v := range values {
		if v != empty {
			return v
		}
	}
	return empty
}
