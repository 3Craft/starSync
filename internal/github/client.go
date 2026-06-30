package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	apiBase    = "https://api.github.com"
	apiVersion = "2022-11-28"
)

// Client 是绑定单个账号 token 的 GitHub REST 客户端。
type Client struct {
	token string
	http  *http.Client
	base  string // 默认 apiBase；测试可覆写
}

// New 用账号 token 构造客户端。
func New(token string) *Client {
	return &Client{token: token, http: &http.Client{Timeout: 30 * time.Second}, base: apiBase}
}

type repo struct {
	FullName string `json:"full_name"`
}

// ListStarred 返回该账号 star 的所有仓库全名（owner/repo），自动翻页。
func (c *Client) ListStarred(ctx context.Context) ([]string, error) {
	var out []string
	url := "/user/starred?per_page=100"
	for url != "" {
		resp, err := c.do(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list starred 失败: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		var repos []repo
		if err := json.Unmarshal(body, &repos); err != nil {
			return nil, err
		}
		for _, r := range repos {
			out = append(out, r.FullName)
		}
		url = nextPage(resp.Header.Get("Link"))
	}
	return out, nil
}

// Star 在当前账号下 star 仓库（已 star 时 GitHub 仍返回 204，幂等）。
func (c *Client) Star(ctx context.Context, fullName string) error {
	return c.starOp(ctx, http.MethodPut, fullName)
}

// Unstar 取消 star。
func (c *Client) Unstar(ctx context.Context, fullName string) error {
	return c.starOp(ctx, http.MethodDelete, fullName)
}

func (c *Client) starOp(ctx context.Context, method, fullName string) error {
	resp, err := c.do(ctx, method, "/user/starred/"+fullName)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %s: %s", method, fullName, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// do 执行请求，遇 secondary rate limit（403/429）按 Retry-After 或指数退避重试。
// path 可为相对路径（拼 base）或绝对 URL（来自 Link header）。
func (c *Client) do(ctx context.Context, method, path string) (*http.Response, error) {
	const maxRetry = 4
	for attempt := 0; ; attempt++ {
		url := path
		if !strings.HasPrefix(path, "http") {
			url = c.base + path
		}
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", apiVersion)

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		if attempt >= maxRetry {
			return resp, nil
		}
		wait := retryAfter(resp.Header.Get("Retry-After"), attempt)
		resp.Body.Close()
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// retryAfter 解析 Retry-After（秒），缺省用指数退避。
func retryAfter(h string, attempt int) time.Duration {
	if h != "" {
		if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil {
			return time.Duration(s) * time.Second
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

// nextPage 从 Link header 解析 rel="next" 的 URL，没有则返回空串。
func nextPage(link string) string {
	for _, part := range strings.Split(link, ",") {
		seg := strings.Split(strings.TrimSpace(part), ";")
		if len(seg) < 2 {
			continue
		}
		u := strings.Trim(strings.TrimSpace(seg[0]), "<>")
		for _, p := range seg[1:] {
			if strings.TrimSpace(p) == `rel="next"` {
				return u
			}
		}
	}
	return ""
}
