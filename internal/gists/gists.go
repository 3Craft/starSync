// Package gists 实现 sync.Syncer，同步 GitHub gist。
//
// Item 语义：gist 的 description（人类可读字段）。
//   - description 为空的 gist 不参与同步（避免"哪个是同一个 gist"的歧义）。
//   - Add 是"内容复制"动作：从 src 拉取完整内容（含 files），
//     在 dst 上创建新 gist。所以 Syncer 需要 src 信息。
//   - Remove 需要 dst 上对应 description 的 gist id。
//
// Syncer 内部维护 snapshots：account → description → gist id，
// 记录 sync 期间最近一次 List 的结果，供 Add/Remove 查询使用。
package gists

import (
	"context"
	"fmt"
	"sync"

	"github.com/3Craft/starSync/internal/github"
	syncpkg "github.com/3Craft/starSync/internal/sync"
)

// TokenFunc 按账号名返回 token（通常为 gh.Client.TokenFor）。
type TokenFunc func(user string) (string, error)

// GistClient 是 gists 同步所需的 GitHub 能力子集，便于测试注入。
type GistClient interface {
	ListGists(ctx context.Context) ([]github.GistMeta, error)
	GetGist(ctx context.Context, id string) (*github.GistDetail, error)
	CreateGist(ctx context.Context, in github.CreateGistInput) (string, error)
	DeleteGist(ctx context.Context, id string) error
}

// Syncer 实现 sync.Syncer，同步 gist。
type Syncer struct {
	token     TokenFunc
	newCli    func(string) GistClient
	clients   sync.Map // user(string) -> GistClient；并发安全
	snapshots sync.Map // user(string) -> map[string]string(description -> id)；并发安全
}

// New 用真实 github 客户端构造 gists Syncer。
func New(token TokenFunc) *Syncer {
	return newWith(token, func(t string) GistClient { return github.New(t) })
}

// newWith 注入自定义客户端构造器，供测试使用。
func newWith(token TokenFunc, newCli func(string) GistClient) *Syncer {
	return &Syncer{token: token, newCli: newCli}
}

// Name 返回资源名。
func (s *Syncer) Name() string { return "gists" }

func (s *Syncer) clientFor(user string) (GistClient, error) {
	if v, ok := s.clients.Load(user); ok {
		return v.(GistClient), nil
	}
	tok, err := s.token(user)
	if err != nil {
		return nil, err
	}
	cl := s.newCli(tok)
	actual, _ := s.clients.LoadOrStore(user, cl)
	return actual.(GistClient), nil
}

// snapshotOf 返回某账号最近一次 List 的 snapshot（description → gist id）。
func (s *Syncer) snapshotOf(user string) (map[string]string, bool) {
	v, ok := s.snapshots.Load(user)
	if !ok {
		return nil, false
	}
	return v.(map[string]string), true
}

// List 拉取账号下全部 gist。description 为空的不进入 Set 与 snapshot。
func (s *Syncer) List(ctx context.Context, a syncpkg.Account) (syncpkg.Set, error) {
	cl, err := s.clientFor(a.User)
	if err != nil {
		return nil, err
	}
	gists, err := cl.ListGists(ctx)
	if err != nil {
		return nil, err
	}
	snap := map[string]string{}
	set := syncpkg.Set{}
	for _, g := range gists {
		if g.Description == "" {
			continue
		}
		snap[g.Description] = g.ID
		set.Add(syncpkg.Item(g.Description))
	}
	s.snapshots.Store(a.User, snap)
	return set, nil
}

// Add 从 src 拉源 gist 完整内容，在 dst 上创建新 gist。
// src 必须先被 List（snapshot 中存在该 description）。
func (s *Syncer) Add(ctx context.Context, src, dst syncpkg.Account, it syncpkg.Item) error {
	desc := string(it)
	srcSnap, ok := s.snapshotOf(src.User)
	if !ok {
		return fmt.Errorf("gists: 缺少源账号 %q 的 snapshot, 请先 List", src.User)
	}
	srcID, ok := srcSnap[desc]
	if !ok {
		return fmt.Errorf("gists: 源账号 %q snapshot 中找不到 %q", src.User, desc)
	}
	srcCli, err := s.clientFor(src.User)
	if err != nil {
		return err
	}
	detail, err := srcCli.GetGist(ctx, srcID)
	if err != nil {
		return err
	}
	dstCli, err := s.clientFor(dst.User)
	if err != nil {
		return err
	}
	files := make(map[string]string, len(detail.Files))
	for name, f := range detail.Files {
		files[name] = f.Content
	}
	_, err = dstCli.CreateGist(ctx, github.CreateGistInput{
		Description: detail.Description,
		Public:      detail.Public,
		Files:       files,
	})
	return err
}

// Remove 删除 dst 上对应 description 的 gist。
// dst 必须先被 List（snapshot 中存在该 description）。
func (s *Syncer) Remove(ctx context.Context, _, dst syncpkg.Account, it syncpkg.Item) error {
	desc := string(it)
	dstSnap, ok := s.snapshotOf(dst.User)
	if !ok {
		return fmt.Errorf("gists: 缺少目标账号 %q 的 snapshot, 请先 List", dst.User)
	}
	dstID, ok := dstSnap[desc]
	if !ok {
		return fmt.Errorf("gists: 目标账号 %q snapshot 中找不到 %q", dst.User, desc)
	}
	dstCli, err := s.clientFor(dst.User)
	if err != nil {
		return err
	}
	return dstCli.DeleteGist(ctx, dstID)
}
