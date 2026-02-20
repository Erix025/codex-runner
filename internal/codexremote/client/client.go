package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codex-runner/internal/shared/jsonutil"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(baseURL string, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type ExecStartRequest struct {
	ProjectID string            `json:"project_id,omitempty"`
	Ref       string            `json:"ref,omitempty"`
	Cmd       string            `json:"cmd"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type ExecStartResponse struct {
	ExecID string `json:"exec_id"`
	Status string `json:"status"`
}

type ExecLogsOptions struct {
	Stream    string
	TailBytes int64
	TailLines int
	Since     string
	Until     string
	Format    string
}

func (c *Client) Health(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("health status: %s", resp.Status)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *Client) ExecStart(ctx context.Context, r ExecStartRequest) (ExecStartResponse, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return ExecStartResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/exec", bytes.NewReader(b))
	if err != nil {
		return ExecStartResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return ExecStartResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return ExecStartResponse{}, fmt.Errorf("exec start failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out ExecStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ExecStartResponse{}, err
	}
	return out, nil
}

func (c *Client) ExecGet(ctx context.Context, execID string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/v1/exec/"+url.PathEscape(execID), nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("exec get failed: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.RawMessage(b), nil
}

func (c *Client) ExecCancel(ctx context.Context, execID string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/exec/"+url.PathEscape(execID)+"/cancel", nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("exec cancel failed: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.RawMessage(b), nil
}

func (c *Client) ExecLogs(ctx context.Context, execID string, opts ExecLogsOptions, w io.Writer) error {
	u := c.BaseURL + "/v1/exec/" + url.PathEscape(execID) + "/logs"
	q := url.Values{}
	if opts.Stream != "" {
		q.Set("stream", opts.Stream)
	}
	if opts.TailBytes >= 0 {
		q.Set("tail", fmt.Sprintf("%d", opts.TailBytes))
	}
	if opts.TailLines > 0 {
		q.Set("tail_lines", fmt.Sprintf("%d", opts.TailLines))
	}
	if opts.Since != "" {
		q.Set("since", opts.Since)
	}
	if opts.Until != "" {
		q.Set("until", opts.Until)
	}
	if opts.Format != "" {
		q.Set("format", opts.Format)
	}
	if strings.Contains(u, "?") {
		u += "&" + q.Encode()
	} else {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	c.addAuth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exec logs failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (c *Client) addAuth(req *http.Request) {
	if c.Token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
}

func PrintJSON(w io.Writer, v any) error {
	return jsonutil.WriteJSON(w, v)
}
