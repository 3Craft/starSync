package following

import (
	"context"
	"testing"

	syncpkg "github.com/3Craft/starSync/internal/sync"
)

type fakeClient struct {
	following  []string
	followed   []string
	unfollowed []string
}

func (f *fakeClient) ListFollowing(context.Context) ([]string, error) {
	return f.following, nil
}
func (f *fakeClient) Follow(_ context.Context, u string) error {
	f.followed = append(f.followed, u)
	return nil
}
func (f *fakeClient) Unfollow(_ context.Context, u string) error {
	f.unfollowed = append(f.unfollowed, u)
	return nil
}

func TestList_MapsUsersToSet(t *testing.T) {
	fc := &fakeClient{following: []string{"alice", "bob"}}
	s := newWith(
		func(u string) (string, error) { return "tok-" + u, nil },
		func(string) FollowingClient { return fc },
	)
	set, err := s.List(context.Background(), syncpkg.Account{User: "me"})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(set) != 2 || !set.Has("alice") || !set.Has("bob") {
		t.Fatalf("Set 映射错误: %v", set)
	}
}

func TestAdd_CallsFollow(t *testing.T) {
	fc := &fakeClient{}
	s := newWith(
		func(string) (string, error) { return "tok", nil },
		func(string) FollowingClient { return fc },
	)
	if err := s.Add(context.Background(), syncpkg.Account{User: "src"}, syncpkg.Account{User: "dst"}, "alice"); err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(fc.followed) != 1 || fc.followed[0] != "alice" {
		t.Fatalf("应调用 Follow(alice), 实际 %v", fc.followed)
	}
}

func TestRemove_CallsUnfollow(t *testing.T) {
	fc := &fakeClient{}
	s := newWith(
		func(string) (string, error) { return "tok", nil },
		func(string) FollowingClient { return fc },
	)
	if err := s.Remove(context.Background(), syncpkg.Account{User: "src"}, syncpkg.Account{User: "dst"}, "alice"); err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(fc.unfollowed) != 1 || fc.unfollowed[0] != "alice" {
		t.Fatalf("应调用 Unfollow(alice), 实际 %v", fc.unfollowed)
	}
}

func TestName(t *testing.T) {
	s := New(func(string) (string, error) { return "", nil })
	if s.Name() != "following" {
		t.Fatalf("Name 应为 following, 实际 %q", s.Name())
	}
}
