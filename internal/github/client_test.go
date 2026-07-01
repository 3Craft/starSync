package github

import (
	"bytes"
	"context"
	"io"
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

func TestListGists_Paginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("Link", `<`+baseFromReq(r)+`/gists?per_page=100&page=2>; rel="next"`)
			w.WriteHeader(200)
			w.Write([]byte(`[{"id":"g1","description":"d1","public":true},{"id":"g2","description":"","public":false}]`))
		default:
			w.WriteHeader(200)
			w.Write([]byte(`[{"id":"g3","description":"d3","public":true}]`))
		}
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	got, err := c.ListGists(context.Background())
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(got) != 3 || got[0].ID != "g1" || got[2].ID != "g3" {
		t.Fatalf("分页结果错误: %+v", got)
	}
}

func TestGetGist_ReturnsDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/gists/abc" {
			t.Fatalf("意外请求: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"abc","description":"d","public":false,"files":{"a.txt":{"content":"hello"}}}`))
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	d, err := c.GetGist(context.Background(), "abc")
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if d.Description != "d" || d.Files["a.txt"].Content != "hello" {
		t.Fatalf("gist 详情解析错误: %+v", d)
	}
}

func TestCreateGist_SendsPOST(t *testing.T) {
	var (
		method  string
		path    string
		gotBody []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
		w.Write([]byte(`{"id":"new1","description":"d","public":true}`))
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	id, err := c.CreateGist(context.Background(), CreateGistInput{
		Description: "d",
		Public:      true,
		Files:       map[string]string{"a.txt": "hi"},
	})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if method != http.MethodPost || path != "/gists" {
		t.Fatalf("请求错误: %s %s", method, path)
	}
	if id != "new1" {
		t.Fatalf("返回 ID 错误: %q", id)
	}
	if !bytes.Contains(gotBody, []byte(`"description":"d"`)) {
		t.Fatalf("请求体未包含 description: %s", gotBody)
	}
}

func TestDeleteGist_Treats404AsSuccess(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	if err := c.DeleteGist(context.Background(), "ghost"); err != nil {
		t.Fatalf("404 应视为幂等成功: %v", err)
	}
}

func TestListFollowing_Paginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("Link", `<`+baseFromReq(r)+`/user/following?per_page=100&page=2>; rel="next"`)
			w.WriteHeader(200)
			w.Write([]byte(`[{"login":"alice"},{"login":"bob"}]`))
		default:
			w.WriteHeader(200)
			w.Write([]byte(`[{"login":"carol"}]`))
		}
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	got, err := c.ListFollowing(context.Background())
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(got) != 3 || got[0] != "alice" || got[2] != "carol" {
		t.Fatalf("分页结果错误: %v", got)
	}
}

func TestFollow_Sends204PUT(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	if err := c.Follow(context.Background(), "alice"); err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if method != http.MethodPut || path != "/user/following/alice" {
		t.Fatalf("请求错误: %s %s", method, path)
	}
}

func TestUnfollow_Sends204DELETE(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New("tok")
	c.base = srv.URL
	if err := c.Unfollow(context.Background(), "alice"); err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if method != http.MethodDelete || path != "/user/following/alice" {
		t.Fatalf("请求错误: %s %s", method, path)
	}
}
