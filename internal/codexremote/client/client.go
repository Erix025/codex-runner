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
	Shell     string            `json:"shell,omitempty"`
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
	Full      bool
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

func (c *Client) ExecRun(ctx context.Context, r ExecStartRequest, w io.Writer) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/exec/run", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuth(req)

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	noTimeout := *hc
	noTimeout.Timeout = 0

	resp, err := noTimeout.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exec run failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	_, err = io.Copy(w, resp.Body)
	return err
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
	if opts.Full {
		q.Set("full", "true")
	} else if opts.TailBytes >= 0 {
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

type FileWriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    int    `json:"mode,omitempty"`
	MkdirP  bool   `json:"mkdir_p,omitempty"`
}

type FileWriteResponse struct {
	OK           bool   `json:"ok"`
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
}

type FileReadResponse struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int    `json:"size"`
}

func (c *Client) FileWrite(ctx context.Context, req FileWriteRequest) (FileWriteResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return FileWriteResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/file/write", bytes.NewReader(body))
	if err != nil {
		return FileWriteResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.addAuth(httpReq)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return FileWriteResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return FileWriteResponse{}, fmt.Errorf("file write failed: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var result FileWriteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return FileWriteResponse{}, err
	}
	return result, nil
}

func (c *Client) FileRead(ctx context.Context, path string) (FileReadResponse, error) {
	body, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return FileReadResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/file/read", bytes.NewReader(body))
	if err != nil {
		return FileReadResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.addAuth(httpReq)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return FileReadResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return FileReadResponse{}, fmt.Errorf("file read failed: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var result FileReadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return FileReadResponse{}, err
	}
	return result, nil
}

func (c *Client) SyncUpload(ctx context.Context, dst string, mkdirP bool, body io.Reader) error {
	u := fmt.Sprintf("%s/v1/sync/upload?dst=%s", c.BaseURL, url.QueryEscape(dst))
	if mkdirP {
		u += "&mkdir_p=true"
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", u, body)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/x-tar+gzip")
	c.addAuth(httpReq)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed (%d): %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Client) SyncDownload(ctx context.Context, src string, excludes []string, w io.Writer) error {
	body, err := json.Marshal(map[string]any{"path": src, "excludes": excludes})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/sync/download", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.addAuth(httpReq)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed (%d): %s", resp.StatusCode, string(b))
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
