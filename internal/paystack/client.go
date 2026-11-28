// Package paystack: minimal client for the Paystack API (ZAR card charging).
// Only the two operations the billing document needs: initialise a hosted
// checkout and verify the resulting transaction. Auth is the secret key;
// amounts are in subunits (cents) per Paystack's contract.
package paystack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const apiBase = "https://api.paystack.co"

type Client struct {
	Secret string
	HTTP   *http.Client
}

func New(secret string) *Client {
	return &Client{Secret: secret, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) call(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("paystack: %w", err)
	}
	defer res.Body.Close()
	var env struct {
		Status  bool            `json:"status"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("paystack: bad response: %s", string(raw[:min(len(raw), 200)]))
	}
	if !env.Status {
		return fmt.Errorf("paystack: %s", env.Message)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// Initialise starts a hosted checkout for `amountCents` (already VAT-inclusive).
func (c *Client) Initialise(ctx context.Context, email string, amountCents int64, currency, reference, callbackURL string) (authorizationURL string, err error) {
	var data struct {
		AuthorizationURL string `json:"authorization_url"`
		Reference        string `json:"reference"`
	}
	in := map[string]any{
		"email":     email,
		"amount":    amountCents,
		"currency":  currency,
		"reference": reference,
	}
	if callbackURL != "" {
		in["callback_url"] = callbackURL
	}
	if err := c.call(ctx, http.MethodPost, "/transaction/initialize", in, &data); err != nil {
		return "", err
	}
	return data.AuthorizationURL, nil
}

type Authorization struct {
	CardType string `json:"card_type"`
	Last4    string `json:"last4"`
	ExpMonth int    `json:"exp_month"`
	ExpYear  int    `json:"exp_year"`
	Bank     string `json:"bank"`
	Reusable bool   `json:"reusable"`
}

type Transaction struct {
	Status        string        `json:"status"` // success | failed | abandoned | pending
	Reference     string        `json:"reference"`
	Amount        int64         `json:"amount"`
	Currency      string        `json:"currency"`
	PaidAt        string        `json:"paid_at"`
	Authorization Authorization `json:"authorization"`
}

// Verify resolves a reference to its final state.
func (c *Client) Verify(ctx context.Context, reference string) (*Transaction, error) {
	var t Transaction
	if err := c.call(ctx, http.MethodGet, "/transaction/verify/"+reference, nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
