# starSync 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现一个单向、只增不删的 GitHub Star 同步 CLI，凭据委托 `gh`，编译为单一 Go 二进制，人类与 Agent 双友好。

**Architecture:** core 引擎只做一件事——`sync(source → target)`：拉两边 star 列表、算差集、把缺的补上（mirror 模式额外删多余）。引擎对 `Syncer` 接口编程，stars 只是第一个实现。凭据通过 shell out 到 `gh auth token` 获取，API 用 Go 原生 `net/http` 直打 GitHub REST。

**Tech Stack:** Go 1.22+、`spf13/cobra`（子命令）、`gopkg.in/yaml.v3`（配置）、标准库 `net/http`（API）、`os/exec`（调 gh）。

## Global Constraints

- **Module path**：`github.com/3Craft/starSync`（执行时可改为用户实际 repo owner，全局替换 import 路径即可）。
- **Go 版本**：≥ 1.22。
- **凭据**：程序不存储任何 token；一律 `gh auth token --user <account>` 现取。
- **流分离**：stdout 只放结果数据；进度、日志、确认提示一律 stderr。
- **退出码**：`0` 全成功 / `1` 运行级错误（参数错、gh 未登录、list 失败）/ `2` 部分条目失败。
- **代码注释语言**：中文（与本项目文档保持一致）；标识符、commit message 用英文。
- **Git**：当前目录**非 git 仓库**。每个 task 末尾以 `go test ./...` 作为验证关口。是否 `git init` / commit 由用户决定——**未经用户要求不要执行任何 git 操作**。
- **YAGNI 偏离记录**：需求文档第 9 节提到的 `--concurrency` flag 本期**不实现**（写操作串行最稳）；留待未来真有性能需求时再加。

---

## 并行开发编排（按 Agent / 依赖轮次 / 文件边界）

依赖方向经过设计，使 Round 1 的四个底层包**零相互依赖、零文件重叠**，可完全并行：

| 轮次 | Agent | Task | 独占文件边界 | 依赖 |
|---|---|---|---|---|
| **Round 0** | 单 agent（阻塞全部） | Task 0 项目骨架 | `go.mod`、目录结构 | 无 |
| **Round 1** | Agent A | Task 1 sync core | `internal/sync/*` | Task 0 |
| **Round 1** | Agent B | Task 2 gh 凭据 | `internal/gh/*` | Task 0 |
| **Round 1** | Agent C | Task 3 github client | `internal/github/*` | Task 0 |
| **Round 1** | Agent D | Task 4 config | `internal/config/*` | Task 0 |
| **Round 2** | Agent E | Task 5 stars syncer | `internal/stars/*` | Task 1、3（用 2 的签名） |
| **Round 3** | Agent F | Task 6 cli + main | `internal/cli/*`、`cmd/starsync/*` | 全部 |

**合并顺序**：Task 0 → Round 1（1/2/3/4 任意序，互不冲突）→ Task 5 → Task 6。Round 1 各包仅在自己目录建文件，合并无冲突；Round 0 已 `go get` 好全部三方依赖，后续 task 不再改 `go.mod`。

---

## File Structure

```
cmd/starsync/main.go        # 入口，os.Exit(cli.Execute())
internal/
  sync/
    types.go                # Account / Item / Set / Mode / Syncer 接口
    report.go               # Report / Failure
    sync.go                 # Sync() 引擎
    sync_test.go            # 用 fakeSyncer 测 union/mirror/dry-run/集合差
  gh/
    gh.go                   # Client.TokenFor / Status（shell out gh）
    gh_test.go              # 注入 mock runner
  github/
    client.go               # REST client：ListStarred/Star/Unstar/分页/退避
    client_test.go          # httptest mock GitHub API
  config/
    config.go               # YAML 解析 + 校验
    config_test.go          # 临时文件测解析与校验
  stars/
    stars.go                # StarsSyncer，实现 sync.Syncer
    stars_test.go           # 注入 fake token + fake StarClient
  cli/
    root.go                 # cobra root + Execute()
    sync.go                 # sync stars 命令 + resolvePairs + runPairs
    doctor.go               # doctor 命令
    version.go              # version 命令
    cli_test.go             # resolvePairs 互斥/校验、退出码映射
docs/
  requirements.md
  implementation-plan.md
go.mod
```

---

## Task 0: 项目骨架（Round 0，阻塞全部）

**Files:**
- Create: `go.mod`、目录结构

**Interfaces:**
- Produces: module `github.com/3Craft/starSync`，已就绪的 `cobra`、`yaml.v3` 依赖；空目录树供各 agent 落文件。

- [ ] **Step 1: 初始化 module 与目录**

```bash
cd /Users/xsharp/Workspace/3Craft/starSync
go mod init github.com/3Craft/starSync
mkdir -p cmd/starsync internal/sync internal/gh internal/github internal/config internal/stars internal/cli
```

- [ ] **Step 2: 拉取三方依赖**

```bash
go get github.com/spf13/cobra@latest
go get gopkg.in/yaml.v3@latest
```

- [ ] **Step 3: 验证 module 就绪**

Run: `go mod verify && go env GOMOD`
Expected: 输出 `all modules verified` 与 `go.mod` 绝对路径，无报错。

---

## Task 1: sync core 引擎（Round 1 / Agent A）

**Files:**
- Create: `internal/sync/types.go`、`internal/sync/report.go`、`internal/sync/sync.go`
- Test: `internal/sync/sync_test.go`

**Interfaces:**
- Produces（后续 task 依赖这些精确签名）:
  - `type Account struct { User string }`
  - `type Item string`、`type Set map[Item]struct{}`，方法 `(Set).Has(Item) bool`、`(Set).Add(Item)`
  - `type Mode int`，常量 `ModeUnion`、`ModeMirror`，`(Mode).String() string`
  - `type Syncer interface { Name() string; List(ctx, Account) (Set, error); Add(ctx, Account, Item) error; Remove(ctx, Account, Item) error }`
  - `type Report struct {...}`、`type Failure struct {...}`、`(Report).HasFailures() bool`
  - `func Sync(ctx context.Context, src, dst Account, s Syncer, mode Mode, dryRun bool) (Report, error)`

- [ ] **Step 1: 写失败测试** — `internal/sync/sync_test.go`

```go
package sync

import (
	"context"
	"errors"
	"testing"
)

// fakeSyncer 是内存实现，用于不触网地测试引擎逻辑。
type fakeSyncer struct {
	data   map[string]Set    // user -> 已有条目
	addErr map[Item]error    // 指定条目 Add 时返回错误
	adds   []string          // 记录 Add 调用 "user:item"
	rms    []string          // 记录 Remove 调用 "user:item"
}

func newFake() *fakeSyncer {
	return &fakeSyncer{data: map[string]Set{}, addErr: map[Item]error{}}
}
func (f *fakeSyncer) Name() string { return "stars" }
func (f *fakeSyncer) set(u string) Set {
	if f.data[u] == nil {
		f.data[u] = Set{}
	}
	return f.data[u]
}
func (f *fakeSyncer) List(_ context.Context, a Account) (Set, error) { return f.set(a.User), nil }
func (f *fakeSyncer) Add(_ context.Context, a Account, it Item) error {
	if e := f.addErr[it]; e != nil {
		return e
	}
	f.set(a.User).Add(it)
	f.adds = append(f.adds, a.User+":"+string(it))
	return nil
}
func (f *fakeSyncer) Remove(_ context.Context, a Account, it Item) error {
	delete(f.set(a.User), it)
	f.rms = append(f.rms, a.User+":"+string(it))
	return nil
}

func TestSync_UnionAddsMissingAndSkipsExisting(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}, "a/2": {}, "a/3": {}}
	f.data["dst"] = Set{"a/2": {}} // 已有 a/2

	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, false)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if rep.SourceCount != 3 || rep.TargetBefore != 1 {
		t.Fatalf("计数错误: %+v", rep)
	}
	if got := len(rep.Added); got != 2 { // a/1, a/3
		t.Fatalf("Added 应为 2, 实际 %d (%v)", got, rep.Added)
	}
	if rep.Skipped != 1 {
		t.Fatalf("Skipped 应为 1, 实际 %d", rep.Skipped)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("union 不应删除, 实际 %v", rep.Removed)
	}
	if !f.set("dst").Has("a/1") || !f.set("dst").Has("a/3") {
		t.Fatalf("dst 未补齐: %v", f.data["dst"])
	}
}

func TestSync_DryRunDoesNotWrite(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}}
	f.data["dst"] = Set{}

	rep, _ := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, true)
	if len(rep.Added) != 1 || !rep.DryRun {
		t.Fatalf("dry-run 应报告 1 项新增: %+v", rep)
	}
	if len(f.adds) != 0 {
		t.Fatalf("dry-run 不应真正写入, 实际调用 %v", f.adds)
	}
}

func TestSync_MirrorRemovesExtraneous(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}}
	f.data["dst"] = Set{"a/1": {}, "a/9": {}} // a/9 是多余的

	rep, _ := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeMirror, false)
	if len(rep.Removed) != 1 || rep.Removed[0] != "a/9" {
		t.Fatalf("mirror 应删除 a/9, 实际 %v", rep.Removed)
	}
	if f.set("dst").Has("a/9") {
		t.Fatalf("a/9 未被删除")
	}
}

func TestSync_ItemFailureRecordedNotFatal(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}, "a/2": {}}
	f.data["dst"] = Set{}
	f.addErr["a/1"] = errors.New("boom")

	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, false)
	if err != nil {
		t.Fatalf("条目失败不应返回顶层 error: %v", err)
	}
	if !rep.HasFailures() || len(rep.Failed) != 1 || rep.Failed[0].Item != "a/1" {
		t.Fatalf("应记录 a/1 失败: %+v", rep.Failed)
	}
	if len(rep.Added) != 1 || rep.Added[0] != "a/2" { // a/2 仍成功
		t.Fatalf("a/2 应成功新增: %v", rep.Added)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/sync/ -run TestSync -v`
Expected: 编译失败 / FAIL（`Sync`、类型未定义）。

- [ ] **Step 3: 实现类型** — `internal/sync/types.go`

```go
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
```

- [ ] **Step 4: 实现 Report** — `internal/sync/report.go`

```go
package sync

// Failure 记录单个条目级失败。
type Failure struct {
	Item  Item   `json:"item"`
	Error string `json:"error"`
}

// Report 是一次单向同步的结果。
type Report struct {
	Resource     string    `json:"resource"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	Mode         string    `json:"mode"`
	DryRun       bool      `json:"dry_run"`
	SourceCount  int       `json:"source_count"`
	TargetBefore int       `json:"target_before"`
	Added        []Item    `json:"added"`
	Removed      []Item    `json:"removed"`
	Skipped      int       `json:"skipped_already"`
	Failed       []Failure `json:"failed"`
}

// HasFailures 报告是否存在条目级失败。
func (r Report) HasFailures() bool { return len(r.Failed) > 0 }
```

- [ ] **Step 5: 实现引擎** — `internal/sync/sync.go`

```go
package sync

import (
	"context"
	"sort"
)

// Sync 执行单向同步：把 src 的资源补齐到 dst。
// union 只增不删；mirror 额外删除 dst 中 src 没有的条目。
// dryRun 为 true 时不执行任何写操作，只计算将要发生的变更。
// 条目级失败记入 Report.Failed 且不中断；仅 List 等运行级错误返回非 nil error。
func Sync(ctx context.Context, src, dst Account, s Syncer, mode Mode, dryRun bool) (Report, error) {
	rep := Report{
		Resource: s.Name(),
		From:     src.User,
		To:       dst.User,
		Mode:     mode.String(),
		DryRun:   dryRun,
		Added:    []Item{},
		Removed:  []Item{},
		Failed:   []Failure{},
	}

	srcSet, err := s.List(ctx, src)
	if err != nil {
		return rep, err
	}
	dstSet, err := s.List(ctx, dst)
	if err != nil {
		return rep, err
	}
	rep.SourceCount = len(srcSet)
	rep.TargetBefore = len(dstSet)

	// 待添加 = src 有、dst 无；排序保证输出与测试确定性。
	var toAdd []Item
	for it := range srcSet {
		if dstSet.Has(it) {
			rep.Skipped++
		} else {
			toAdd = append(toAdd, it)
		}
	}
	sortItems(toAdd)
	for _, it := range toAdd {
		if dryRun {
			rep.Added = append(rep.Added, it)
			continue
		}
		if err := s.Add(ctx, dst, it); err != nil {
			rep.Failed = append(rep.Failed, Failure{Item: it, Error: err.Error()})
			continue
		}
		rep.Added = append(rep.Added, it)
	}

	if mode == ModeMirror {
		var toRemove []Item
		for it := range dstSet {
			if !srcSet.Has(it) {
				toRemove = append(toRemove, it)
			}
		}
		sortItems(toRemove)
		for _, it := range toRemove {
			if dryRun {
				rep.Removed = append(rep.Removed, it)
				continue
			}
			if err := s.Remove(ctx, dst, it); err != nil {
				rep.Failed = append(rep.Failed, Failure{Item: it, Error: err.Error()})
				continue
			}
			rep.Removed = append(rep.Removed, it)
		}
	}

	return rep, nil
}

func sortItems(xs []Item) {
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}
```

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/sync/ -v`
Expected: 全部 PASS。

---

## Task 2: gh 凭据包（Round 1 / Agent B）

**Files:**
- Create: `internal/gh/gh.go`
- Test: `internal/gh/gh_test.go`

**Interfaces:**
- Produces:
  - `type Client struct {...}`、`func New() *Client`
  - `func (c *Client) TokenFor(user string) (string, error)` — Task 5 以 `ghc.TokenFor` 形式作为 `TokenFunc` 传入
  - `func (c *Client) Status() (string, error)` — Task 6 doctor 使用

- [ ] **Step 1: 写失败测试** — `internal/gh/gh_test.go`

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gh/ -v`
Expected: 编译失败（`Client`、`output`、`TokenFor` 未定义）。

- [ ] **Step 3: 实现** — `internal/gh/gh.go`

```go
package gh

import (
	"fmt"
	"os/exec"
	"strings"
)

// runner 执行外部命令，便于测试注入。
type runner func(name string, args ...string) ([]byte, error)

// Client 通过 gh CLI 获取凭据，自身不存储任何 token。
type Client struct {
	output   runner // 仅 stdout，用于取 token
	combined runner // stdout+stderr，用于诊断
}

// New 返回调用真实 gh 命令的 Client。
func New() *Client {
	return &Client{
		output:   func(n string, a ...string) ([]byte, error) { return exec.Command(n, a...).Output() },
		combined: func(n string, a ...string) ([]byte, error) { return exec.Command(n, a...).CombinedOutput() },
	}
}

// TokenFor 返回指定账号的 token；无状态，不切换 gh 的 active 账号。
func (c *Client) TokenFor(user string) (string, error) {
	out, err := c.output("gh", "auth", "token", "--user", user)
	if err != nil {
		return "", fmt.Errorf("获取账号 %q 的 token 失败（请确认已执行 gh auth login）：%w", user, err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf("账号 %q 的 token 为空", user)
	}
	return tok, nil
}

// Status 返回 gh auth status 的合并输出，供 doctor 诊断展示。
func (c *Client) Status() (string, error) {
	out, err := c.combined("gh", "auth", "status")
	return string(out), err
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gh/ -v`
Expected: 全部 PASS。

---

## Task 3: github REST client（Round 1 / Agent C）

**Files:**
- Create: `internal/github/client.go`
- Test: `internal/github/client_test.go`

**Interfaces:**
- Produces:
  - `func New(token string) *Client`
  - `func (c *Client) ListStarred(ctx context.Context) ([]string, error)` — 返回 `owner/repo` 列表，自动翻页
  - `func (c *Client) Star(ctx context.Context, fullName string) error`
  - `func (c *Client) Unstar(ctx context.Context, fullName string) error`
  - 内部字段 `base string` 供测试注入 mock server 地址。

- [ ] **Step 1: 写失败测试** — `internal/github/client_test.go`

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/github/ -v`
Expected: 编译失败（`New`、`ListStarred` 等未定义）。

- [ ] **Step 3: 实现** — `internal/github/client.go`

```go
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	apiBase    = "https://api.github.com"
	apiVersion = "2022-11-28"
)

// Client 是绑定单个账号 token 的 GitHub REST 客户端。
type Client struct {
	token string
	http  *http.Client
	base  string // 默认 apiBase；测试可覆写
}

// New 用账号 token 构造客户端。
func New(token string) *Client {
	return &Client{token: token, http: &http.Client{Timeout: 30 * time.Second}, base: apiBase}
}

type repo struct {
	FullName string `json:"full_name"`
}

// ListStarred 返回该账号 star 的所有仓库全名（owner/repo），自动翻页。
func (c *Client) ListStarred(ctx context.Context) ([]string, error) {
	var out []string
	url := "/user/starred?per_page=100"
	for url != "" {
		resp, err := c.do(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list starred 失败: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		var repos []repo
		if err := json.Unmarshal(body, &repos); err != nil {
			return nil, err
		}
		for _, r := range repos {
			out = append(out, r.FullName)
		}
		url = nextPage(resp.Header.Get("Link"))
	}
	return out, nil
}

// Star 在当前账号下 star 仓库（已 star 时 GitHub 仍返回 204，幂等）。
func (c *Client) Star(ctx context.Context, fullName string) error {
	return c.starOp(ctx, http.MethodPut, fullName)
}

// Unstar 取消 star。
func (c *Client) Unstar(ctx context.Context, fullName string) error {
	return c.starOp(ctx, http.MethodDelete, fullName)
}

func (c *Client) starOp(ctx context.Context, method, fullName string) error {
	resp, err := c.do(ctx, method, "/user/starred/"+fullName)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %s: %s", method, fullName, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// do 执行请求，遇 secondary rate limit（403/429）按 Retry-After 或指数退避重试。
// path 可为相对路径（拼 base）或绝对 URL（来自 Link header）。
func (c *Client) do(ctx context.Context, method, path string) (*http.Response, error) {
	const maxRetry = 4
	for attempt := 0; ; attempt++ {
		url := path
		if !strings.HasPrefix(path, "http") {
			url = c.base + path
		}
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", apiVersion)

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		if attempt >= maxRetry {
			return resp, nil
		}
		wait := retryAfter(resp.Header.Get("Retry-After"), attempt)
		resp.Body.Close()
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// retryAfter 解析 Retry-After（秒），缺省用指数退避。
func retryAfter(h string, attempt int) time.Duration {
	if h != "" {
		if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil {
			return time.Duration(s) * time.Second
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

// nextPage 从 Link header 解析 rel="next" 的 URL，没有则返回空串。
func nextPage(link string) string {
	for _, part := range strings.Split(link, ",") {
		seg := strings.Split(strings.TrimSpace(part), ";")
		if len(seg) < 2 {
			continue
		}
		u := strings.Trim(strings.TrimSpace(seg[0]), "<>")
		for _, p := range seg[1:] {
			if strings.TrimSpace(p) == `rel="next"` {
				return u
			}
		}
	}
	return ""
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/github/ -v`
Expected: 全部 PASS。

---

## Task 4: config 配置解析（Round 1 / Agent D）

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Pair struct { From string; To []string; Mirror bool }`
  - `type Config struct { Syncs []Pair }`
  - `func Load(path string) (*Config, error)`

- [ ] **Step 1: 写失败测试** — `internal/config/config_test.go`

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "syncs.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ParsesSyncs(t *testing.T) {
	p := writeTemp(t, `
syncs:
  - from: xsharp
    to: [justdn, trendcms]
  - from: trendcms
    to: [xsharp]
    mirror: true
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(cfg.Syncs) != 2 {
		t.Fatalf("应解析 2 组, 实际 %d", len(cfg.Syncs))
	}
	if cfg.Syncs[0].From != "xsharp" || len(cfg.Syncs[0].To) != 2 {
		t.Fatalf("第一组解析错误: %+v", cfg.Syncs[0])
	}
	if !cfg.Syncs[1].Mirror {
		t.Fatalf("第二组 mirror 应为 true")
	}
}

func TestLoad_RejectsEmptyFrom(t *testing.T) {
	p := writeTemp(t, "syncs:\n  - to: [a]\n")
	if _, err := Load(p); err == nil {
		t.Fatal("from 为空应报错")
	}
}

func TestLoad_RejectsEmptySyncs(t *testing.T) {
	p := writeTemp(t, "syncs: []\n")
	if _, err := Load(p); err == nil {
		t.Fatal("空 syncs 应报错")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -v`
Expected: 编译失败（`Load`、`Config` 未定义）。

- [ ] **Step 3: 实现** — `internal/config/config.go`

```go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Pair 是一组单向同步映射：from 的 star 同步给 to 列表中每个账号。
type Pair struct {
	From   string   `yaml:"from"`
	To     []string `yaml:"to"`
	Mirror bool     `yaml:"mirror"`
}

// Config 是配置文件根结构。只描述映射关系，不含任何 token。
type Config struct {
	Syncs []Pair `yaml:"syncs"`
}

// Load 读取并校验 YAML 配置文件。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置 %q 失败: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %q 失败: %w", path, err)
	}
	if len(cfg.Syncs) == 0 {
		return nil, fmt.Errorf("配置 %q 中 syncs 为空", path)
	}
	for i, p := range cfg.Syncs {
		if p.From == "" {
			return nil, fmt.Errorf("syncs[%d].from 不能为空", i)
		}
		if len(p.To) == 0 {
			return nil, fmt.Errorf("syncs[%d].to 不能为空", i)
		}
	}
	return &cfg, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: 全部 PASS。

---

## Task 5: stars Syncer（Round 2 / Agent E）

**Files:**
- Create: `internal/stars/stars.go`
- Test: `internal/stars/stars_test.go`

**Interfaces:**
- Consumes: `sync.{Account,Item,Set,Syncer}`（Task 1）；`github.New` 提供的 `ListStarred/Star/Unstar`（Task 3）；`gh.Client.TokenFor` 的签名 `func(string)(string,error)`（Task 2）。
- Produces:
  - `type TokenFunc func(user string) (string, error)`
  - `type StarClient interface { ListStarred(ctx) ([]string, error); Star(ctx, string) error; Unstar(ctx, string) error }`
  - `func New(token TokenFunc) *Syncer`（实现 `sync.Syncer`）
  - 测试用构造器 `func newWith(token TokenFunc, client func(string) StarClient) *Syncer`

- [ ] **Step 1: 写失败测试** — `internal/stars/stars_test.go`

```go
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
	if err := s.Add(context.Background(), syncpkg.Account{User: "bob"}, "a/9"); err != nil {
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/stars/ -v`
Expected: 编译失败（`New`、`newWith`、`StarClient` 未定义）。

- [ ] **Step 3: 实现** — `internal/stars/stars.go`

```go
package stars

import (
	"context"

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
	clients map[string]StarClient // 按账号缓存，避免重复取 token / 建连接
}

// New 用真实 github 客户端构造 stars Syncer。
func New(token TokenFunc) *Syncer {
	return newWith(token, func(t string) StarClient { return github.New(t) })
}

// newWith 注入自定义客户端构造器，供测试使用。
func newWith(token TokenFunc, newCli func(string) StarClient) *Syncer {
	return &Syncer{token: token, newCli: newCli, clients: map[string]StarClient{}}
}

// Name 返回资源名。
func (s *Syncer) Name() string { return "stars" }

// clientFor 返回某账号的客户端，带缓存。
func (s *Syncer) clientFor(user string) (StarClient, error) {
	if cl, ok := s.clients[user]; ok {
		return cl, nil
	}
	tok, err := s.token(user)
	if err != nil {
		return nil, err
	}
	cl := s.newCli(tok)
	s.clients[user] = cl
	return cl, nil
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

// Add 在目标账号 star 指定仓库。
func (s *Syncer) Add(ctx context.Context, a syncpkg.Account, it syncpkg.Item) error {
	cl, err := s.clientFor(a.User)
	if err != nil {
		return err
	}
	return cl.Star(ctx, string(it))
}

// Remove 在目标账号 unstar 指定仓库（仅 mirror 模式）。
func (s *Syncer) Remove(ctx context.Context, a syncpkg.Account, it syncpkg.Item) error {
	cl, err := s.clientFor(a.User)
	if err != nil {
		return err
	}
	return cl.Unstar(ctx, string(it))
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/stars/ -v`
Expected: 全部 PASS。

---

## Task 6: CLI 组装 + 入口（Round 3 / Agent F）

**Files:**
- Create: `internal/cli/root.go`、`internal/cli/sync.go`、`internal/cli/doctor.go`、`internal/cli/version.go`、`cmd/starsync/main.go`
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `gh.New().TokenFor`、`stars.New`、`sync.Sync`、`config.Load`。
- Produces: `func Execute() int`（返回进程退出码）。

- [ ] **Step 1: 写失败测试** — `internal/cli/cli_test.go`

```go
package cli

import "testing"

func TestResolvePairs_FlagFanout(t *testing.T) {
	ps, err := resolvePairs("src", []string{"a", "b"}, "", false)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(ps) != 2 || ps[0].from != "src" || ps[0].to != "a" || ps[1].to != "b" {
		t.Fatalf("展开错误: %+v", ps)
	}
}

func TestResolvePairs_ConfigAndFlagsMutuallyExclusive(t *testing.T) {
	if _, err := resolvePairs("src", []string{"a"}, "cfg.yaml", false); err == nil {
		t.Fatal("--config 与 --from/--to 并用应报错")
	}
}

func TestResolvePairs_RequiresFromAndTo(t *testing.T) {
	if _, err := resolvePairs("", nil, "", false); err == nil {
		t.Fatal("缺少 from/to 应报错")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -v`
Expected: 编译失败（`resolvePairs` 未定义）。

- [ ] **Step 3: 实现 root** — `internal/cli/root.go`

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// exitError 承载期望的进程退出码。
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// Execute 构建并运行 CLI，返回进程退出码。
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root := &cobra.Command{
		Use:           "starsync",
		Short:         "在多个 GitHub 账号之间同步 star",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newSyncCmd(), newDoctorCmd(), newVersionCmd())

	if err := root.ExecuteContext(ctx); err != nil {
		if ec, ok := err.(*exitError); ok {
			return ec.code
		}
		fmt.Fprintln(os.Stderr, "错误:", err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: 实现 sync 命令** — `internal/cli/sync.go`

```go
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/3Craft/starSync/internal/config"
	"github.com/3Craft/starSync/internal/gh"
	"github.com/3Craft/starSync/internal/stars"
	syncpkg "github.com/3Craft/starSync/internal/sync"
)

// pair 是展开后的单向同步任务。
type pair struct {
	from   string
	to     string
	mirror bool
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sync", Short: "同步资源"}
	cmd.AddCommand(newSyncStarsCmd())
	return cmd
}

func newSyncStarsCmd() *cobra.Command {
	var (
		from    string
		to      []string
		cfgPath string
		mirror  bool
		dryRun  bool
		asJSON  bool
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "stars",
		Short: "同步 starred 仓库",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pairs, err := resolvePairs(from, to, cfgPath, mirror)
			if err != nil {
				fmt.Fprintln(os.Stderr, "错误:", err)
				return &exitError{code: 1}
			}
			ghc := gh.New()
			syncer := stars.New(ghc.TokenFor)
			return runPairs(cmd.Context(), syncer, pairs, dryRun, asJSON, yes)
		},
	}
	f := cmd.Flags()
	f.StringVar(&from, "from", "", "源账号")
	f.StringArrayVar(&to, "to", nil, "目标账号（可重复）")
	f.StringVar(&cfgPath, "config", "", "多对多配置文件路径")
	f.BoolVar(&mirror, "mirror", false, "镜像模式（含 unstar，破坏性）")
	f.BoolVar(&dryRun, "dry-run", false, "只预演不写入")
	f.BoolVar(&asJSON, "json", false, "输出 JSON Report")
	f.BoolVar(&yes, "yes", false, "跳过交互确认")
	return cmd
}

// resolvePairs 把 flag / 配置展开为单向 pair 列表。
func resolvePairs(from string, to []string, cfgPath string, mirror bool) ([]pair, error) {
	if cfgPath != "" {
		if from != "" || len(to) > 0 {
			return nil, fmt.Errorf("--config 不能与 --from/--to 同时使用")
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return nil, err
		}
		var ps []pair
		for _, p := range cfg.Syncs {
			for _, t := range p.To {
				ps = append(ps, pair{from: p.From, to: t, mirror: p.Mirror})
			}
		}
		return ps, nil
	}
	if from == "" || len(to) == 0 {
		return nil, fmt.Errorf("必须提供 --from 与至少一个 --to（或使用 --config）")
	}
	var ps []pair
	for _, t := range to {
		ps = append(ps, pair{from: from, to: t, mirror: mirror})
	}
	return ps, nil
}

// runPairs 串行执行所有 pair，按结果决定退出码。
func runPairs(ctx context.Context, syncer syncpkg.Syncer, pairs []pair, dryRun, asJSON, yes bool) error {
	var reports []syncpkg.Report
	var runErr, itemFail bool

	for _, p := range pairs {
		mode := syncpkg.ModeUnion
		if p.mirror {
			mode = syncpkg.ModeMirror
			if !dryRun && !yes && !confirmMirror(p.from, p.to) {
				fmt.Fprintf(os.Stderr, "跳过 %s → %s（用户取消镜像）\n", p.from, p.to)
				continue
			}
		}
		fmt.Fprintf(os.Stderr, "同步 stars: %s → %s（%s）\n", p.from, p.to, mode)
		rep, err := syncpkg.Sync(ctx, syncpkg.Account{User: p.from}, syncpkg.Account{User: p.to}, syncer, mode, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  失败: %v\n", err)
			runErr = true
			continue
		}
		if rep.HasFailures() {
			itemFail = true
		}
		reports = append(reports, rep)
		if !asJSON {
			printHuman(rep)
		}
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			fmt.Fprintln(os.Stderr, "JSON 编码失败:", err)
			return &exitError{code: 1}
		}
	}

	switch {
	case runErr:
		return &exitError{code: 1}
	case itemFail:
		return &exitError{code: 2}
	}
	return nil
}

// confirmMirror 在 stderr 提示并从 stdin 读取确认。
func confirmMirror(from, to string) bool {
	fmt.Fprintf(os.Stderr, "⚠️  镜像模式将取消 %s 上 %s 没有 star 的仓库。输入 yes 确认: ", to, from)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line) == "yes"
}

// printHuman 把 Report 摘要打到 stdout（结果数据）。
func printHuman(r syncpkg.Report) {
	mark := ""
	if r.DryRun {
		mark = "[dry-run] "
	}
	fmt.Printf("%s%s → %s: 源 %d, 目标原有 %d, 新增 %d, 跳过 %d, 删除 %d, 失败 %d\n",
		mark, r.From, r.To, r.SourceCount, r.TargetBefore, len(r.Added), r.Skipped, len(r.Removed), len(r.Failed))
	for _, f := range r.Failed {
		fmt.Printf("  失败: %s (%s)\n", f.Item, f.Error)
	}
}
```

- [ ] **Step 5: 实现 doctor 与 version** — `internal/cli/doctor.go`

```go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/3Craft/starSync/internal/gh"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "检查 gh 登录状态与账号可用性",
		RunE: func(_ *cobra.Command, _ []string) error {
			out, err := gh.New().Status()
			fmt.Fprint(os.Stderr, out) // 诊断信息走 stderr
			if err != nil {
				return &exitError{code: 1}
			}
			return nil
		},
	}
}
```

`internal/cli/version.go`：

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version 可在构建时通过 -ldflags "-X .../cli.Version=x.y.z" 注入。
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出版本号",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println(Version)
			return nil
		},
	}
}
```

- [ ] **Step 6: 实现入口** — `cmd/starsync/main.go`

```go
package main

import (
	"os"

	"github.com/3Craft/starSync/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
```

- [ ] **Step 7: 跑测试与构建确认通过**

Run: `go test ./... && go build -o /dev/null ./cmd/starsync`
Expected: 全部 PASS，构建成功无报错。

- [ ] **Step 8: 冒烟验证（真实环境，可选）**

Run: `go run ./cmd/starsync sync stars --from xsharp --to justdn --dry-run`
Expected: stderr 打印 `同步 stars: xsharp → justdn（union）`，stdout 打印 `[dry-run]` 摘要，进程退出码 0。`go run ./cmd/starsync doctor` 应列出 gh 已登录账号。

---

## 最终验证（全部 task 完成后）

- [ ] `go vet ./...` 无告警
- [ ] `go test ./...` 全绿
- [ ] `go build -o starsync ./cmd/starsync` 产出二进制
- [ ] `./starsync sync stars --from xsharp --to justdn --dry-run` 行为符合预期
- [ ] `./starsync sync stars --config docs/example-syncs.yaml --dry-run` 行为符合预期（如创建示例配置）
```
