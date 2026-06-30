# starSync 需求与设计文档

> 单向、只增不删的 GitHub Star 同步 CLI。凭据委托 `gh`，Go 单二进制，人类 & Agent 双友好。

- **文档日期**：2026-06-30
- **状态**：设计已确认，待进入实现计划

---

## 1. 背景与目标

用户拥有多个 GitHub 账号（且对目标账号有完整权限），希望把在某个账号 star 的仓库同步到其他账号，让多个账号的 star 列表保持一致。

### 目标（In Scope）

1. 把**源账号**的 starred 仓库同步到**一个或多个目标账号**。
2. 默认采用**只增不删**语义，永不误删目标账号已有的 star。
3. 凭据完全委托给本机已安装的 `gh`（GitHub CLI），程序自身不存储任何 token。
4. CLI 同时对**人类**和 **Agent**友好（结构化输出、明确退出码、非交互模式）。
5. 架构对未来扩展（gist / following 等其他可同步资源）保持开放，但本期不实现。

### 非目标（Out of Scope）

- ❌ 定时 / 常驻轮询：交给系统 `cron` / `launchd` 或上层 Agent 调度。
- ❌ 同步 gist、following、watching 等非 repo-star 资源（架构留口子，不实现）。
- ❌ 自行管理 / 存储 GitHub 凭据。
- ❌ 图形界面。

---

## 2. 术语

| 术语 | 含义 |
|---|---|
| **源账号（source / from）** | star 的来源账号，作为对照基准 |
| **目标账号（target / to）** | 被同步、需要补齐 star 的账号 |
| **union（并集 / 只增不删）** | 默认模式：source 有、target 无的补上；target 多出来的保留 |
| **mirror（镜像）** | 破坏性模式：让 target 与 source 完全一致，source 没有的在 target 上 unstar |
| **Syncer** | "一类可同步 GitHub 资源"的抽象接口，Stars 是其第一个实现 |

---

## 3. 核心设计原则

整个程序围绕**一个原子原语**构建：

```
sync(source → target):
    1. 拉取 source 的全部 starred 仓库  → 集合 S
    2. 拉取 target 的全部 starred 仓库  → 集合 T
    3. union  模式：对 (S - T) 中每个 repo，在 target 上 star
       mirror 模式：额外对 (T - S) 中每个 repo，在 target 上 unstar
    4. 汇总 Report
```

**关键特性**：

- **无状态**：每次都现拉两边列表算差集，不依赖本地数据库或上次运行记录。
- **幂等**：重复运行结果稳定，已 star 的不会重复操作。
- **单一职责**：core 引擎只做"单向同步"这一件事，复杂拓扑全靠上层参数 / 配置拼装。

> 设计依据：KISS（核心极简）、单一职责（core 只管单向）、YAGNI（不做定时 / 多资源）。

---

## 4. 拓扑结构

所有拓扑都是单向原语 `sync(source → target)` 的组合，core 引擎无需感知拓扑：

| 拓扑 | 实现方式 | 命令示例 |
|---|---|---|
| 一对一 | 单次原语 | `starsync sync stars --from xsharp --to justdn` |
| 一对多 | 对多个 target 循环 | `starsync sync stars --from xsharp --to justdn --to trendcms` |
| 多对多 / 双向 | 配置文件列多组映射 | `starsync sync stars --config syncs.yaml` |

---

## 5. 同步语义

| 模式 | flag | 行为 | 风险 |
|---|---|---|---|
| **union（默认）** | 无 | source 有、target 无的补 star；target 多出的保留不动 | 安全 |
| **mirror** | `--mirror` | 额外 unstar target 上 source 没有的 repo | ⚠️ 破坏性 |

**mirror 安全护栏**：

- 默认进入**交互式二次确认**，明确列出将被 unstar 的仓库数量与清单。
- `--yes`：跳过确认（供非交互 / Agent 使用）。
- `--dry-run`：只预演、不执行任何写操作，输出将要发生的变更。

---

## 6. 凭据管理：完全委托 gh

程序**不存储、不管理任何 token**。凭据来源为本机已登录的 `gh`：

- 取凭据：`gh auth token --user <account>` —— **无状态**地获取任意已登录账号的 token，无需切换 `gh` 的全局 active 账号（避免有状态副作用与并发竞争）。
- 校验：`starsync doctor` 透传 `gh auth status`，展示各账号登录状态与 token scopes 供人工核对（含是否含 `repo`）。

**前置条件**：

- 本机已安装 `gh`（≥ 2.95.0 验证可用）。
- 涉及的所有账号均已通过 `gh auth login` 登录，且 token 具备 `repo` scope。

---

## 7. 技术栈与实现

| 层 | 技术 | 说明 |
|---|---|---|
| 语言 | **Go** | 单一静态二进制，零运行时依赖，下载即用 |
| CLI 框架 | `cobra` | 子命令、自动 help，便于扩展新 `sync <resource>` |
| 凭据 | shell out 到 `gh auth token` | 仅把 `gh` 当"钥匙串代理" |
| API 调用 | Go 原生 `net/http` | 直打 GitHub REST API，自主掌控分页 / 并发 / 退避 / JSON 输出 |
| 配置解析 | YAML | 多对多映射配置 |

**为什么 API 不也走 `gh api`**：自己用 `net/http` 能精确控制分页、写操作限流退避、结构化输出，且不被 `gh` 的输出格式绑架。`gh` 只负责"取 token"这一件最适合它的事。

---

## 8. 可扩展抽象（Syncer 接口）

为未来同步 gist / following 等资源留口子，core 引擎仅依赖接口编程（开闭原则 + 依赖倒置）：

```go
// Syncer 抽象"一类可同步的 GitHub 资源"。Stars 是第一个实现。
type Syncer interface {
    Name() string                                          // "stars" / "gists" / "following"
    List(ctx context.Context, acct Account) (Set, error)   // 拉取某账号下全部条目
    Add(ctx context.Context, acct Account, item Item) error
    Remove(ctx context.Context, acct Account, item Item) error // 仅 mirror 模式使用
}

// core 引擎对任意 Syncer 通用，完全不知道自己在同步什么
func Sync(ctx context.Context, src, dst Account, s Syncer, mode Mode) (Report, error)
```

- **本期只实现 `StarsSyncer`**。
- 未来扩展：写新 Syncer 实现 → 注册 → CLI 自动多出 `starsync sync <name>`，**core 引擎一行不改**。

---

## 9. CLI 规格

```
starsync sync stars --from <account> --to <account> [--to <account>...] [flags]
starsync sync stars --config <file.yaml> [flags]
starsync doctor
starsync version
```

### Flags（`sync stars`）

| flag | 类型 | 默认 | 说明 |
|---|---|---|---|
| `--from` | string | — | 源账号（gh 已登录的用户名）；用 `--config` 时不需要 |
| `--to` | string（可重复） | — | 一个或多个目标账号 |
| `--config` | string | — | 多对多映射配置文件路径；与 `--from/--to` 互斥 |
| `--mirror` | bool | false | 启用镜像模式（含 unstar，破坏性） |
| `--dry-run` | bool | false | 只预演不写入 |
| `--json` | bool | false | stdout 输出结构化 JSON Report |
| `--yes` | bool | false | 跳过所有交互确认（非交互 / Agent） |

> 注：写操作当前**串行**执行（最稳），不暴露并发 flag；并发是未来扩展，见第 16 节。

---

## 10. Agent 友好契约

| 维度 | 设计 |
|---|---|
| **流分离** | **stdout = 结果数据；stderr = 进度 / 日志**，保证管道安全 |
| **结构化输出** | `--json` 时 stdout 输出机器可读 Report |
| **退出码** | `0` 全部成功 / `1` 运行错误（如 gh 未登录、参数错误）/ `2` 部分条目失败 |
| **非交互** | `--dry-run` 预演、`--yes` 免确认 |
| **自检** | `starsync doctor` 透传 `gh auth status`，展示各账号登录状态与 token scopes |
| **幂等** | 重复运行结果稳定，无副作用累积 |

### JSON Report 结构

```json
{
  "from": "xsharp",
  "to": "justdn",
  "mode": "union",
  "dry_run": false,
  "source_count": 152,
  "target_before": 30,
  "added": ["owner/repo", "..."],
  "removed": [],
  "skipped_already": 28,
  "failed": [
    { "item": "owner/private-repo", "error": "404 Not Found（目标账号无访问权）" }
  ],
  "counts": { "added": 122, "removed": 0, "failed": 1 }
}
```

> 一对多 / 多对多时，stdout 输出 Report 数组，每个 source→target 一项。

---

## 11. 速率限制与分页

- **分页**：list starred 用 `GET /user/starred?per_page=100`，按 `Link` header 翻页直至取完。
- **写操作限流**：`PUT/DELETE /user/starred/{owner}/{repo}`（成功返回 204）。GitHub 对快速写操作有 **secondary rate limit**，因此：
  - 默认**串行**执行写操作，最稳（并发暂未实现，见第 16 节）。
  - 遇 `403 / 429` 时读取 `Retry-After`，做**指数退避 + 重试**。
- 认证后主 rate limit 为 5000 req/h，同步数百个 star 在额度内。

---

## 12. 配置文件规格（多对多）

`syncs.yaml`，**只描述映射关系，不含任何 token**：

```yaml
syncs:
  - from: xsharp
    to: [justdn, trendcms]
  - from: trendcms
    to: [xsharp]
    mirror: true   # 可选，按组覆盖模式，默认 union
```

---

## 13. 错误处理边界

| 场景 | 处理 |
|---|---|
| `gh` 未安装 / 账号未登录 | 启动时友好报错，`exit 1` |
| 账号缺 `repo` scope | `doctor` 提示；运行时私有 repo star 失败归入 `failed[]` |
| 单个 repo star 失败 | **不中断整体**，记入 `failed[]`，结束时 `exit 2` |
| 私有 repo 目标账号无访问权 | star 返回 404，归入 `failed[]` 并附原因 |
| `--from/--to` 与 `--config` 同时给出 | 参数冲突，`exit 1` |

---

## 14. 项目结构

```
starSync/
  cmd/starsync/main.go        # 入口
  internal/
    cli/      # cobra 命令定义 & flag 解析
    sync/     # core 引擎 Sync() + Syncer 接口 + Report 类型 + Mode
    github/   # REST client：分页 list / star / unstar / 退避重试
    gh/       # gh auth token 包装（凭据获取）
    config/   # 多对多 YAML 配置解析
  docs/
    requirements.md
  go.mod
```

---

## 15. 测试策略

| 对象 | 方法 |
|---|---|
| core `Sync()` | 用 **fake Syncer**（内存实现）单测 union / mirror / dry-run / 集合差逻辑，不触网 |
| `github` client | 用 `httptest` mock GitHub API，测分页、204 处理、403/429 退避 |
| `gh` 凭据层 | 包一层接口，mock `gh auth token` 输出 |
| CLI | 测参数解析、互斥校验、退出码 |

---

## 16. 未来扩展（YAGNI，仅记录方向）

- `starsync sync gists`：新增 `GistSyncer` 实现。
- `starsync sync following`：新增 `FollowingSyncer` 实现。
- 二者均只需新增文件 + 注册，core 引擎不变。
- `--concurrency`：写操作并发。当前串行最稳；真有性能需求时再加（需配合 GitHub secondary rate limit 的限流退避）。
```
