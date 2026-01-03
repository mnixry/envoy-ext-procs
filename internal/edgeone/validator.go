package edgeone

import (
	"context"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/samber/oops"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	teo "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/teo/v20220901"
	"golang.org/x/sync/singleflight"
)

type Config struct {
	SecretID    string
	SecretKey   string
	APIEndpoint string
	Region      string
	CacheSize   int
	CacheTTL    time.Duration
	Timeout     time.Duration
}

type Validator struct {
	cache  *expirable.LRU[string, bool]
	client *teo.Client
	sg     singleflight.Group
}

func New(cfg Config) (*Validator, error) {
	if strings.TrimSpace(cfg.SecretID) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, oops.
			In("edgeone").
			Code("MISSING_CREDENTIALS").
			Errorf("missing SecretID or SecretKey")
	}

	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = cfg.APIEndpoint
	cpf.HttpProfile.ReqTimeout = int(cfg.Timeout.Seconds())

	credential := common.NewCredential(cfg.SecretID, cfg.SecretKey)
	client, err := teo.NewClient(credential, cfg.Region, cpf)
	if err != nil {
		return nil, oops.
			In("edgeone").
			Code("CLIENT_INIT_FAILED").
			With("region", cfg.Region).
			With("endpoint", cfg.APIEndpoint).
			Wrapf(err, "failed to create tencent teo client")
	}

	return &Validator{
		cache:  expirable.NewLRU[string, bool](cfg.CacheSize, nil, cfg.CacheTTL),
		client: client,
	}, nil
}

func (v *Validator) IsEdgeOneIP(ctx context.Context, ip netip.Addr) (bool, error) {
	ip = ip.Unmap()
	ipStr := ip.String()

	if cached, ok := v.cache.Get(ipStr); ok {
		return cached, nil
	}

	// EdgeOne IPs are public; private/loopback can never be EdgeOne.
	if !ip.IsGlobalUnicast() {
		return false, nil
	}

	val, err, _ := v.sg.Do(ipStr, func() (any, error) {
		if cached, ok := v.cache.Get(ipStr); ok {
			return cached, nil
		}
		valid, err := v.validateIP(ctx, ip)
		if err != nil {
			return false, err
		}
		v.cache.Add(ipStr, valid)
		return valid, nil
	})
	return val.(bool), err
}

func (v *Validator) validateIP(ctx context.Context, ip netip.Addr) (bool, error) {
	req := teo.NewDescribeIPRegionRequest()
	req.IPs = []*string{common.StringPtr(ip.String())}

	resp, err := v.client.DescribeIPRegionWithContext(ctx, req)
	if err != nil {
		return false, oops.
			In("edgeone").
			Code("API_REQUEST_FAILED").
			With("ip", ip.String()).
			Wrapf(err, "failed to describe IP region")
	}

	return slices.ContainsFunc(resp.Response.IPRegionInfo, func(info *teo.IPRegionInfo) bool {
		return strings.EqualFold(*info.IsEdgeOneIP, "yes")
	}), nil
}
