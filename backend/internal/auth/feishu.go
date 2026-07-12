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

const (
	FeishuRegionChina  = "cn"
	FeishuRegionGlobal = "global"
)

type FeishuConfig struct {
	Enabled                               bool
	Region, AppID, AppSecret, RedirectURL string
}
type FeishuProfile struct{ Subject, Email, DisplayName, Department string }
type FeishuService struct {
	cfg    FeishuConfig
	client *http.Client
	state  *OIDCService
}

func NewFeishuService(cfg FeishuConfig) (*FeishuService, error) {
	cfg.Region = strings.ToLower(strings.TrimSpace(cfg.Region))
	if cfg.Region == "" {
		cfg.Region = FeishuRegionChina
	}
	if cfg.Region != FeishuRegionChina && cfg.Region != FeishuRegionGlobal {
		return nil, errors.New("feishu region must be cn or global")
	}
	if cfg.Enabled && (strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" || strings.TrimSpace(cfg.RedirectURL) == "") {
		return nil, errors.New("feishu app id, app secret, and redirect url are required")
	}
	state, _ := NewOIDCService(OIDCConfig{Enabled: cfg.Enabled, IssuerURL: "https://" + strings.TrimPrefix(strings.TrimPrefix(cfg.RedirectURL, "https://"), "http://"), ClientID: cfg.AppID, RedirectURL: cfg.RedirectURL})
	return &FeishuService{cfg: cfg, client: http.DefaultClient, state: state}, nil
}

func (s *FeishuService) Begin(now time.Time) (OIDCState, error) { return s.state.Begin(now) }
func (s *FeishuService) Consume(value string, now time.Time) (OIDCState, error) {
	return s.state.Consume(value, now)
}

func (s *FeishuService) baseURL() string {
	if s.cfg.Region == FeishuRegionGlobal {
		return "https://open.larksuite.com"
	}
	return "https://open.feishu.cn"
}
func (s *FeishuService) Region() string { return s.cfg.Region }
func (s *FeishuService) AuthorizationURL(state, verifier string) string {
	v := url.Values{"app_id": {s.cfg.AppID}, "redirect_uri": {s.cfg.RedirectURL}, "response_type": {"code"}, "state": {state}, "scope": {"contact:user.base:readonly"}}
	if verifier != "" {
		v.Set("code_challenge", verifier)
		v.Set("code_challenge_method", "S256")
	}
	return s.baseURL() + "/open-apis/authen/v1/authorize?" + v.Encode()
}

func (s *FeishuService) Complete(ctx context.Context, code, verifier string) (FeishuProfile, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {s.cfg.AppID}, "client_secret": {s.cfg.AppSecret}, "code": {code}}
	if strings.TrimSpace(verifier) != "" {
		form.Set("code_verifier", verifier)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL()+"/open-apis/authen/v1/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return FeishuProfile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return FeishuProfile{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return FeishuProfile{}, fmt.Errorf("feishu token exchange failed: http %d", resp.StatusCode)
	}
	var token struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &token); err != nil {
		return FeishuProfile{}, err
	}
	if token.Code != 0 || token.Data.AccessToken == "" {
		return FeishuProfile{}, fmt.Errorf("feishu token exchange failed: %s", token.Msg)
	}
	infoReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL()+"/open-apis/authen/v1/user_info", nil)
	infoReq.Header.Set("Authorization", "Bearer "+token.Data.AccessToken)
	infoResp, err := s.client.Do(infoReq)
	if err != nil {
		return FeishuProfile{}, err
	}
	defer infoResp.Body.Close()
	infoRaw, _ := io.ReadAll(io.LimitReader(infoResp.Body, 1<<20))
	var info struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID     string `json:"open_id"`
			UnionID    string `json:"union_id"`
			Email      string `json:"email"`
			Name       string `json:"name"`
			Department string `json:"department_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(infoRaw, &info); err != nil {
		return FeishuProfile{}, err
	}
	if info.Code != 0 {
		return FeishuProfile{}, fmt.Errorf("feishu user info failed: %s", info.Msg)
	}
	sub := info.Data.UnionID
	if sub == "" {
		sub = info.Data.OpenID
	}
	if sub == "" {
		return FeishuProfile{}, errors.New("feishu user info has no stable subject")
	}
	return FeishuProfile{Subject: sub, Email: strings.ToLower(strings.TrimSpace(info.Data.Email)), DisplayName: strings.TrimSpace(info.Data.Name), Department: strings.TrimSpace(info.Data.Department)}, nil
}
