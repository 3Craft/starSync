// Package following 实现 sync.Syncer，同步 GitHub following（关注列表）。
//
// Item 语义：被关注者的 GitHub 用户名（unique 天然成立）。
// 与 stars 同型："标记型"同步——Add/Remove 不需要 src 内容，
// 只需 dst 的 token 在 dst 上 PUT/DELETE。
package following

import (
	"context"
	"sync"

	"github.com/3Craft/starSync/internal/github"
	syncpkg "github.com/3Craft/starSync/internal/sync"
)

// TokenFunc 按账号名返回 token（通常为 gh.Client.TokenFor）。
type TokenFunc func(user string) (string, error)

// FollowingClient 是 following 同步所需的 GitHub 能力子集，便于测试注入。
type FollowingClient interface {
	ListFollowing(ctx context.Context) ([]string, error)
	Follow(ctx context.Context, username string) error
	Unfollow(ctx context.Context, username string) error
}

// Syncer 实现 sync.Syncer，同步关注列表。
type Syncer struct {
	token   TokenFunc
	newCli  func(token string) FollowingClient
	clients sync.Map // user(string) -> FollowingClient；并发安全
}

// New 用真实 github 客户端构造 following Syncer。
func New(token TokenFunc) *Syncer {
	return newWith(token, func(t string) FollowingClient { return github.New(t) })
}

// newWith 注入自定义客户端构造器，供测试使用。
func newWith(token TokenFunc, newCli func(string) FollowingClient) *Syncer {
	return &Syncer{token: token, newCli: newCli}
}

// Name 返回资源名。
func (s *Syncer) Name() string { return "following" }

func (s *Syncer) clientFor(user string) (FollowingClient, error) {
	if v, ok := s.clients.Load(user); ok {
		return v.(FollowingClient), nil
	}
	tok, err := s.token(user)
	if err != nil {
		return nil, err
	}
	cl := s.newCli(tok)
	actual, _ := s.clients.LoadOrStore(user, cl)
	return actual.(FollowingClient), nil
}

// List 拉取账号关注的用户名，映射为 Set。
func (s *Syncer) List(ctx context.Context, a syncpkg.Account) (syncpkg.Set, error) {
	cl, err := s.clientFor(a.User)
	if err != nil {
		return nil, err
	}
	users, err := cl.ListFollowing(ctx)
	if err != nil {
		return nil, err
	}
	set := syncpkg.Set{}
	for _, u := range users {
		set.Add(syncpkg.Item(u))
	}
	return set, nil
}

// Add 在目标账号关注指定用户。src 参数 unused（"标记型"同步不需要）。
func (s *Syncer) Add(ctx context.Context, _, dst syncpkg.Account, it syncpkg.Item) error {
	cl, err := s.clientFor(dst.User)
	if err != nil {
		return err
	}
	return cl.Follow(ctx, string(it))
}

// Remove 在目标账号取消关注指定用户。src 参数 unused。
func (s *Syncer) Remove(ctx context.Context, _, dst syncpkg.Account, it syncpkg.Item) error {
	cl, err := s.clientFor(dst.User)
	if err != nil {
		return err
	}
	return cl.Unfollow(ctx, string(it))
}
