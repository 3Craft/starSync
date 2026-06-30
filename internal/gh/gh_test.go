package gh

import (
	"errors"
	"testing"
)

func TestTokenFor_TrimsOutput(t *testing.T) {
	c := &Client{
		output: func(name string, args ...string) ([]byte, error) {
			// 校验调用的是无状态的 --user 形式
			want := []string{"auth", "token", "--user", "alice"}
			for i, w := range want {
				if args[i] != w {
					t.Fatalf("参数错误: %v", args)
				}
			}
			return []byte("gho_abc123\n"), nil
		},
	}
	tok, err := c.TokenFor("alice")
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if tok != "gho_abc123" {
		t.Fatalf("token 应被 trim, 实际 %q", tok)
	}
}

func TestTokenFor_ErrorWrapped(t *testing.T) {
	c := &Client{output: func(string, ...string) ([]byte, error) {
		return nil, errors.New("not logged in")
	}}
	if _, err := c.TokenFor("ghost"); err == nil {
		t.Fatal("未登录账号应返回错误")
	}
}

func TestTokenFor_EmptyTokenIsError(t *testing.T) {
	c := &Client{output: func(string, ...string) ([]byte, error) {
		return []byte("  \n"), nil
	}}
	if _, err := c.TokenFor("alice"); err == nil {
		t.Fatal("空 token 应返回错误")
	}
}
