package extproc

import (
	"net/netip"
	"strings"

	"github.com/samber/oops"
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

func FirstNonEmpty[T comparable](values ...T) T {
	var empty T
	for _, v := range values {
		if v != empty {
			return v
		}
	}
	return empty
}

func FirstNonEmptyFn[T comparable](factories ...func() (T, error)) (T, error) {
	var empty T
	for _, factory := range factories {
		if v, err := factory(); err != nil {
			return empty, err
		} else if v != empty {
			return v, nil
		}
	}
	return empty, oops.New("no non-empty value found")
}
