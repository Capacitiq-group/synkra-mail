// oner.go — the production wire driver for the Oner domains reseller API.
//
// EVERY endpoint path and response shape in this file is marked
// TODO(oner-verify): https://docs.oner.co.za was unreachable from the build
// sandbox (connection timeout) while this integration was written, so the
// paths model the documented operation set ("core domain lifecycle operations
// across supported TLDs") and standard registrar-API practice. Verify against
// the live docs before enabling a real key — nothing above this adapter
// changes when the paths are corrected.
package registrar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OnerHTTP struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func NewOner(baseURL, apiKey string) *OnerHTTP {
	return &OnerHTTP{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// call executes one authenticated JSON request and decodes the response.
// TODO(oner-verify): auth header shape (Bearer vs X-API-Key), error envelope.
func (o *OnerHTTP) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, o.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := o.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("oner: %s %s: %d %s", method, path, res.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

type onerCheckResp struct {
	Domain    string  `json:"domain"`
	Available bool    `json:"available"`
	Price     float64 `json:"price"`
}

func (o *OnerHTTP) CheckAvailable(ctx context.Context, names []string) ([]Check, error) {
	var resp struct {
		Results []onerCheckResp `json:"results"`
	}
	// TODO(oner-verify): bulk check endpoint name.
	if err := o.call(ctx, http.MethodPost, "/domains/check", map[string]any{"domains": names}, &resp); err != nil {
		return nil, err
	}
	out := make([]Check, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, Check{Name: r.Domain, Available: r.Available, PriceZAR: r.Price})
	}
	return out, nil
}

func (o *OnerHTTP) Register(ctx context.Context, name string, years int, registrant RegistrantContact, nameservers []string) (*DomainInfo, error) {
	var info DomainInfo
	// TODO(oner-verify): registration endpoint + payload keys.
	in := map[string]any{
		"domain":      name,
		"years":       years,
		"registrant":  registrant,
		"nameservers": nameservers,
		"auto_renew":  true,
	}
	if err := o.call(ctx, http.MethodPost, "/domains/register", in, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (o *OnerHTTP) Info(ctx context.Context, name string) (*DomainInfo, error) {
	var info DomainInfo
	// TODO(oner-verify): info endpoint + response keys.
	if err := o.call(ctx, http.MethodGet, "/domains/"+name, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (o *OnerHTTP) SetNameservers(ctx context.Context, name string, ns []string) error {
	// TODO(oner-verify): nameserver update endpoint.
	return o.call(ctx, http.MethodPut, "/domains/"+name+"/nameservers", map[string]any{"nameservers": ns}, nil)
}

func (o *OnerHTTP) SetTransferLock(ctx context.Context, name string, enabled bool) error {
	// TODO(oner-verify): transfer-lock endpoint.
	return o.call(ctx, http.MethodPut, "/domains/"+name+"/transfer-lock", map[string]any{"enabled": enabled}, nil)
}

func (o *OnerHTTP) SetWhoisPrivacy(ctx context.Context, name string, enabled bool) error {
	// TODO(oner-verify): privacy endpoint.
	return o.call(ctx, http.MethodPut, "/domains/"+name+"/whois-privacy", map[string]any{"enabled": enabled}, nil)
}

func (o *OnerHTTP) SetAutoRenew(ctx context.Context, name string, enabled bool) error {
	// TODO(oner-verify): auto-renew endpoint.
	return o.call(ctx, http.MethodPut, "/domains/"+name+"/auto-renew", map[string]any{"enabled": enabled}, nil)
}

func (o *OnerHTTP) SetRegistrant(ctx context.Context, name string, registrant RegistrantContact) error {
	// TODO(oner-verify): registrant update endpoint (triggers registry
	// contact verification for .co.za).
	return o.call(ctx, http.MethodPut, "/domains/"+name+"/registrant", map[string]any{"registrant": registrant}, nil)
}

func (o *OnerHTTP) Renew(ctx context.Context, name string, years int) (*time.Time, error) {
	var resp struct {
		RenewsUntil time.Time `json:"renews_until"`
	}
	// TODO(oner-verify): renewal endpoint.
	if err := o.call(ctx, http.MethodPost, "/domains/"+name+"/renew", map[string]any{"years": years}, &resp); err != nil {
		return nil, err
	}
	return &resp.RenewsUntil, nil
}

func (o *OnerHTTP) EPPCode(ctx context.Context, name string) (string, error) {
	var resp struct {
		EPPCode string `json:"epp_code"`
	}
	// TODO(oner-verify): auth-code endpoint.
	if err := o.call(ctx, http.MethodPost, "/domains/"+name+"/epp", nil, &resp); err != nil {
		return "", err
	}
	return resp.EPPCode, nil
}

func (o *OnerHTTP) TransferOut(ctx context.Context, name, eppCode string) error {
	// TODO(oner-verify): transfer-away endpoint.
	return o.call(ctx, http.MethodPost, "/domains/"+name+"/transfer-out", map[string]any{"epp_code": eppCode}, nil)
}
