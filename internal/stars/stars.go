package stars

import (
	"context"
	"sync"

	"github.com/3Craft/starSync/internal/github"
	syncpkg "github.com/3Craft/starSync/internal/sync"
)

// TokenFunc 按账号名返回 token（通常为 gh.Client.TokenFor）。
type TokenFunc func(user string) (string, error)

// StarClient 是 stars 同步所需的 GitHub 能力子集，便于测试注入。
type StarClient interface {
	ListStarred(ctx context.Context) ([]string, error)
	Star(ctx context.Context, fullName string) error
	Unstar(ctx context.Context, fullName string) error
}

// Syncer 实现 sync.Syncer，同步 starred 仓库。
type Syncer struct {
	token   TokenFunc
	newCli  func(token string) StarClient
	clients sync.Map // user(string) -> StarClient；并发安全，支持 --concurrency
}

// New 用真实 github 客户端构造 stars Syncer。
func New(token TokenFunc) *Syncer {
	return newWith(token, func(t string) StarClient { return github.New(t) })
}

// newWith 注入自定义客户端构造器，供测试使用。
func newWith(token TokenFunc, newCli func(string) StarClient) *Syncer {
	return &Syncer{token: token, newCli: newCli}
}

// Name 返回资源名。
func (s *Syncer) Name() string { return "stars" }

// clientFor 返回某账号的客户端，带缓存。
func (s *Syncer) clientFor(user string) (StarClient, error) {
	if v, ok := s.clients.Load(user); ok {
		return v.(StarClient), nil
	}
	tok, err := s.token(user)
	if err != nil {
		return nil, err
	}
	cl := s.newCli(tok)
	actual, _ := s.clients.LoadOrStore(user, cl)
	return actual.(StarClient), nil
}

// List 拉取账号下全部 star，映射为 Set。
func (s *Syncer) List(ctx context.Context, a syncpkg.Account) (syncpkg.Set, error) {
	cl, err := s.clientFor(a.User)
	if err != nil {
		return nil, err
	}
	repos, err := cl.ListStarred(ctx)
	if err != nil {
		return nil, err
	}
	set := syncpkg.Set{}
	for _, r := range repos {
		set.Add(syncpkg.Item(r))
	}
	return set, nil
}

// Add 在目标账号 star 指定仓库。src 是占位参数（stars 同步不需要源账号内容）。
func (s *Syncer) Add(ctx context.Context, _, a syncpkg.Account, it syncpkg.Item) error {
	cl, err := s.clientFor(a.User)
	if err != nil {
		return err
	}
	return cl.Star(ctx, string(it))
}

// Remove 在目标账号 unstar 指定仓库（仅 mirror 模式）。src 同 Add。
func (s *Syncer) Remove(ctx context.Context, _, a syncpkg.Account, it syncpkg.Item) error {
	cl, err := s.clientFor(a.User)
	if err != nil {
		return err
	}
	return cl.Unstar(ctx, string(it))
}
