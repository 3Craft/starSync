package sync

import "context"

// Account 代表一个 gh 已登录的 GitHub 账号。
type Account struct {
	User string
}

// Item 是可同步条目的唯一标识。对 stars 而言是 "owner/repo"。
type Item string

// Set 是 Item 的去重集合。
type Set map[Item]struct{}

// Has 报告集合是否包含 i。
func (s Set) Has(i Item) bool { _, ok := s[i]; return ok }

// Add 把 i 加入集合。
func (s Set) Add(i Item) { s[i] = struct{}{} }

// Mode 是同步模式。
type Mode int

const (
	ModeUnion  Mode = iota // 只增不删
	ModeMirror             // 镜像：含删除
)

// String 返回模式的可读名称。
func (m Mode) String() string {
	if m == ModeMirror {
		return "mirror"
	}
	return "union"
}

// Syncer 抽象"一类可同步的 GitHub 资源"。Stars 是第一个实现。
type Syncer interface {
	Name() string
	List(ctx context.Context, acct Account) (Set, error)
	Add(ctx context.Context, acct Account, item Item) error
	Remove(ctx context.Context, acct Account, item Item) error
}
