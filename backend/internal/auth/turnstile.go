package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type TurnstileVerifier struct {
	Client   *http.Client
	Endpoint string
}

func (v TurnstileVerifier) Verify(ctx context.Context, secret, response, remoteIP string) error {
	secret, response = strings.TrimSpace(secret), strings.TrimSpace(response)
	if secret == "" {
		return errors.New("turnstile secret is not configured")
	}
	if response == "" {
		return errors.New("turnstile response is required")
	}
	endpoint := v.Endpoint
	if endpoint == "" {
		endpoint = turnstileVerifyURL
	}
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	form := url.Values{"secret": {secret}, "response": {response}}
	if strings.TrimSpace(remoteIP) != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("turnstile verification returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Success bool     `json:"success"`
		Errors  []string `json:"error-codes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return err
	}
	if !result.Success {
		if len(result.Errors) > 0 {
			return errors.New("turnstile verification failed: " + strings.Join(result.Errors, ","))
		}
		return errors.New("turnstile verification failed")
	}
	return nil
}
