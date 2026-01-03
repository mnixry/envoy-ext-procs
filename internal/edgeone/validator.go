package edgeone

import (
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/rs/zerolog"
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
	log    zerolog.Logger
}

func New(cfg Config, log zerolog.Logger) (*Validator, error) {
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
		log:    log.With().Str("component", "edgeone").Logger(),
	}, nil
}

func (v *Validator) IsEdgeOneIP(ip netip.Addr) (bool, error) {
	ip = ip.Unmap()
	ipStr := ip.String()

	if cached, ok := v.cache.Get(ipStr); ok {
		return cached, nil
	}

	val, err, _ := v.sg.Do(ipStr, func() (any, error) {
		if cached, ok := v.cache.Get(ipStr); ok {
			return cached, nil
		}
		start := time.Now()
		valid, err := v.validateIP(ip)
		if err != nil {
			return false, err
		}
		v.log.Info().
			Dur("duration", time.Since(start)).
			Str("ip", ipStr).
			Bool("valid", valid).
			Msg("IP region validation result")
		v.cache.Add(ipStr, valid)
		return valid, nil
	})
	return val.(bool), err
}

func (v *Validator) validateIP(ip netip.Addr) (bool, error) {
	// EdgeOne IPs are public; private/loopback can never be EdgeOne.
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false, nil
	}

	req := teo.NewDescribeIPRegionRequest()
	req.IPs = []*string{common.StringPtr(ip.String())}

	resp, err := v.client.DescribeIPRegion(req)
	if err != nil {
		return false, oops.
			In("edgeone").
			Code("API_REQUEST_FAILED").
			With("ip", ip.String()).
			Wrapf(err, "failed to describe IP region")
	}

	validated := slices.ContainsFunc(resp.Response.IPRegionInfo, func(info *teo.IPRegionInfo) bool {
		return strings.EqualFold(*info.IsEdgeOneIP, "yes")
	})
	v.log.Debug().
		Str("ip", ip.String()).
		Bool("valid", validated).
		Interface("request", req).
		Interface("response", resp).
		Msg("IP region validation result")
	return validated, nil
}
