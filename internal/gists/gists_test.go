package gists

import (
	"context"
	"errors"
	"testing"

	"github.com/3Craft/starSync/internal/github"
	syncpkg "github.com/3Craft/starSync/internal/sync"
)

// fakeClient 是 GistClient 的内存实现。
type fakeClient struct {
	gists   []github.GistMeta
	details map[string]*github.GistDetail
	created []github.CreateGistInput
	deleted []string
	failGet bool
	failCrt bool
}

func newFake() *fakeClient {
	return &fakeClient{details: map[string]*github.GistDetail{}}
}

func (f *fakeClient) ListGists(context.Context) ([]github.GistMeta, error) {
	return f.gists, nil
}
func (f *fakeClient) GetGist(_ context.Context, id string) (*github.GistDetail, error) {
	if f.failGet {
		return nil, errors.New("get fail")
	}
	return f.details[id], nil
}
func (f *fakeClient) CreateGist(_ context.Context, in github.CreateGistInput) (string, error) {
	if f.failCrt {
		return "", errors.New("create fail")
	}
	f.created = append(f.created, in)
	return "new-id", nil
}
func (f *fakeClient) DeleteGist(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func TestList_SkipsEmptyDescriptionAndCachesSnapshot(t *testing.T) {
	fc := newFake()
	fc.gists = []github.GistMeta{
		{ID: "g1", Description: "config", Public: true},
		{ID: "g2", Description: "", Public: false}, // 空描述应跳过
		{ID: "g3", Description: "snippet", Public: true},
	}
	s := newWith(
		func(u string) (string, error) { return "tok-" + u, nil },
		func(string) GistClient { return fc },
	)
	set, err := s.List(context.Background(), syncpkg.Account{User: "alice"})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(set) != 2 || !set.Has("config") || !set.Has("snippet") {
		t.Fatalf("Set 应只有非空描述的 gist: %v", set)
	}
	snap, _ := s.snapshots.Load("alice")
	got := snap.(map[string]string)
	if got["config"] != "g1" || got["snippet"] != "g3" {
		t.Fatalf("snapshot 映射错误: %+v", got)
	}
	if _, ok := got[""]; ok {
		t.Fatalf("空描述不应进入 snapshot")
	}
}

func TestAdd_CopiesContentFromSrcToDst(t *testing.T) {
	srcCli := newFake()
	srcCli.gists = []github.GistMeta{{ID: "g1", Description: "config", Public: false}}
	srcCli.details["g1"] = &github.GistDetail{
		ID:          "g1",
		Description: "config",
		Public:      false,
		Files:       map[string]github.GistFile{"a.txt": {Content: "hello"}},
	}
	dstCli := newFake()

	// Syncer 内部以 token 为 key 构造 client，所以这里按 token 索引。
	// tokenFunc 返回 "tok-alice" / "tok-bob"。
	clientsByToken := map[string]GistClient{"tok-alice": srcCli, "tok-bob": dstCli}
	s := newWith(
		func(u string) (string, error) { return "tok-" + u, nil },
		func(t string) GistClient { return clientsByToken[t] },
	)
	if _, err := s.List(context.Background(), syncpkg.Account{User: "alice"}); err != nil {
		t.Fatalf("src.List: %v", err)
	}
	if _, err := s.List(context.Background(), syncpkg.Account{User: "bob"}); err != nil {
		t.Fatalf("dst.List: %v", err)
	}
	if err := s.Add(context.Background(), syncpkg.Account{User: "alice"}, syncpkg.Account{User: "bob"}, "config"); err != nil {
		t.Fatalf("Add 意外错误: %v", err)
	}
	if len(dstCli.created) != 1 {
		t.Fatalf("dst 应创建 1 个 gist, 实际 %d", len(dstCli.created))
	}
	in := dstCli.created[0]
	if in.Description != "config" || in.Public != false {
		t.Fatalf("描述/可见性未保留: %+v", in)
	}
	if in.Files["a.txt"] != "hello" {
		t.Fatalf("files 未复制: %+v", in.Files)
	}
}

func TestAdd_FailsWithoutSrcSnapshot(t *testing.T) {
	fc := newFake()
	s := newWith(func(string) (string, error) { return "tok", nil }, func(string) GistClient { return fc })
	err := s.Add(context.Background(), syncpkg.Account{User: "alice"}, syncpkg.Account{User: "bob"}, "config")
	if err == nil {
		t.Fatalf("缺少 snapshot 时 Add 应报错")
	}
}

func TestRemove_DeletesByDstSnapshot(t *testing.T) {
	dstCli := newFake()
	dstCli.gists = []github.GistMeta{{ID: "g9", Description: "old", Public: true}}
	clientsByToken := map[string]GistClient{"tok-alice": newFake(), "tok-bob": dstCli}
	s := newWith(
		func(u string) (string, error) { return "tok-" + u, nil },
		func(t string) GistClient { return clientsByToken[t] },
	)
	if _, err := s.List(context.Background(), syncpkg.Account{User: "alice"}); err != nil {
		t.Fatalf("src.List: %v", err)
	}
	if _, err := s.List(context.Background(), syncpkg.Account{User: "bob"}); err != nil {
		t.Fatalf("dst.List: %v", err)
	}
	if err := s.Remove(context.Background(), syncpkg.Account{User: "alice"}, syncpkg.Account{User: "bob"}, "old"); err != nil {
		t.Fatalf("Remove 意外错误: %v", err)
	}
	if len(dstCli.deleted) != 1 || dstCli.deleted[0] != "g9" {
		t.Fatalf("应删除 g9, 实际 %v", dstCli.deleted)
	}
}

func TestName(t *testing.T) {
	s := New(func(string) (string, error) { return "", nil })
	if s.Name() != "gists" {
		t.Fatalf("Name 应为 gists, 实际 %q", s.Name())
	}
}
