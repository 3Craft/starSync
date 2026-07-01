package stars

import (
	"context"
	"testing"

	syncpkg "github.com/3Craft/starSync/internal/sync"
)

// fakeClient 是 StarClient 的内存实现。
type fakeClient struct {
	stars   []string
	starred []string
}

func (f *fakeClient) ListStarred(context.Context) ([]string, error) { return f.stars, nil }
func (f *fakeClient) Star(_ context.Context, full string) error {
	f.starred = append(f.starred, full)
	return nil
}
func (f *fakeClient) Unstar(context.Context, string) error { return nil }

func TestList_MapsReposToSet(t *testing.T) {
	fc := &fakeClient{stars: []string{"a/1", "a/2"}}
	s := newWith(
		func(u string) (string, error) { return "tok-" + u, nil },
		func(string) StarClient { return fc },
	)
	set, err := s.List(context.Background(), syncpkg.Account{User: "alice"})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(set) != 2 || !set.Has("a/1") || !set.Has("a/2") {
		t.Fatalf("Set 映射错误: %v", set)
	}
}

func TestAdd_CallsStar(t *testing.T) {
	fc := &fakeClient{}
	s := newWith(
		func(string) (string, error) { return "tok", nil },
		func(string) StarClient { return fc },
	)
	if err := s.Add(context.Background(), syncpkg.Account{User: "alice"}, syncpkg.Account{User: "bob"}, "a/9"); err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(fc.starred) != 1 || fc.starred[0] != "a/9" {
		t.Fatalf("应调用 Star(a/9), 实际 %v", fc.starred)
	}
}

func TestName(t *testing.T) {
	s := New(func(string) (string, error) { return "", nil })
	if s.Name() != "stars" {
		t.Fatalf("Name 应为 stars, 实际 %q", s.Name())
	}
}
