package gh

import (
	"fmt"
	"os/exec"
	"strings"
)

// runner 执行外部命令，便于测试注入。
type runner func(name string, args ...string) ([]byte, error)

// Client 通过 gh CLI 获取凭据，自身不存储任何 token。
type Client struct {
	output   runner // 仅 stdout，用于取 token
	combined runner // stdout+stderr，用于诊断
}

// New 返回调用真实 gh 命令的 Client。
func New() *Client {
	return &Client{
		output:   func(n string, a ...string) ([]byte, error) { return exec.Command(n, a...).Output() },
		combined: func(n string, a ...string) ([]byte, error) { return exec.Command(n, a...).CombinedOutput() },
	}
}

// TokenFor 返回指定账号的 token；无状态，不切换 gh 的 active 账号。
func (c *Client) TokenFor(user string) (string, error) {
	out, err := c.output("gh", "auth", "token", "--user", user)
	if err != nil {
		return "", fmt.Errorf("获取账号 %q 的 token 失败（请确认已执行 gh auth login）：%w", user, err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf("账号 %q 的 token 为空", user)
	}
	return tok, nil
}

// Status 返回 gh auth status 的合并输出，供 doctor 诊断展示。
func (c *Client) Status() (string, error) {
	out, err := c.combined("gh", "auth", "status")
	return string(out), err
}
