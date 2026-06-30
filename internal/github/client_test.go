package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListStarred_Paginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("缺少 Bearer token: %q", got)
		}
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("Link", `<`+baseFromReq(r)+`/user/starred?per_page=100&page=2>; rel="next"`)
			w.WriteHeader(200)
			w.Write([]byte(`[{"full_name":"a/1"},{"full_name":"a/2"}]`))
		default:
			w.WriteHeader(200)
			w.Write([]byte(`[{"full_name":"a/3"}]`))
		}
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	got, err := c.ListStarred(context.Background())
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(got) != 3 || got[0] != "a/1" || got[2] != "a/3" {
		t.Fatalf("分页结果错误: %v", got)
	}
}

func TestStar_Sends204PUT(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	if err := c.Star(context.Background(), "a/1"); err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if method != http.MethodPut || path != "/user/starred/a/1" {
		t.Fatalf("请求错误: %s %s", method, path)
	}
}

func TestDo_RetriesOnRateLimit(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0") // 立即重试，避免测试等待
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	if err := c.Star(context.Background(), "a/1"); err != nil {
		t.Fatalf("退避重试后应成功: %v", err)
	}
	if calls != 2 {
		t.Fatalf("应重试一次, 实际调用 %d 次", calls)
	}
}

// baseFromReq 在测试中重建 mock server 的 base URL。
func baseFromReq(r *http.Request) string { return "http://" + r.Host }
