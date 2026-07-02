# starSync

在多个 GitHub 账号之间同步 star 的命令行工具。**单向、只增不删**，凭据完全委托 `gh`，编译为单一 Go 二进制，人类与 Agent 双友好。

## 特性

- **零凭据管理**：不存储任何 token，运行时通过 `gh auth token --user <账号>` 现取现用。
- **安全默认**：默认 `union`（只增不删），永不误删目标账号已有的 star；镜像（含 unstar）需 `--mirror` 显式开启并二次确认。
- **任意拓扑**：一对一 / 一对多 / 多对多 / 双向，全靠参数与配置组合，底层只有一个单向同步原语。
- **Agent 友好**：stdout 放结果、stderr 放日志；`--json` 结构化输出；明确退出码；`--dry-run` / `--yes` 非交互。
- **幂等无状态**：每次现拉两边列表算差集，无本地数据库，重复运行结果稳定。

## 前置条件

- 已安装 [`gh`](https://cli.github.com/)（GitHub CLI，≥ 2.95.0）。
- 涉及的所有账号均已 `gh auth login` 登录，且 token 具备 `repo` scope。
- Go ≥ 1.22（仅构建时需要）。

用 `starsync doctor` 检查 gh 登录状态与各账号 token scopes。

## 安装

### Homebrew（macOS）

```bash
brew tap 3Craft/starsync
brew trust 3Craft/starsync      # 新版 Homebrew 需信任第三方 tap 才能安装 cask
brew install --cask starsync
```

> Homebrew cask 仅支持 macOS；Linux 用户请用下方的 `go install` 或预编译二进制。

### `go install`

```bash
go install github.com/3Craft/starSync@latest
```

### 下载预编译二进制

去 [Releases](https://github.com/3Craft/starSync/releases) 挑自己平台的 archive：

```bash
# 示例：macOS Apple Silicon
curl -L https://github.com/3Craft/starSync/releases/latest/download/starsync_VERSION_darwin_arm64.tar.gz | tar xz
sudo mv starsync /usr/local/bin/

# 验证签名（可选，但推荐）
curl -L https://github.com/3Craft/starSync/releases/latest/download/starsync_checksums.txt
shasum -a 256 -c starsync_checksums.txt
```

## 从源码构建

```bash
go build -o starsync ./cmd/starsync
```

## 用法

### 一对一

```bash
starsync sync stars --from xsharp --to justdn
starsync sync gists --from xsharp --to justdn
starsync sync following --from xsharp --to justdn
```

### 一对多

```bash
starsync sync stars --from xsharp --to justdn --to trendcms
starsync sync following --from xsharp --to justdn --to trendcms
```

### 多对多 / 双向（配置文件）

```bash
starsync sync stars     --config docs/example-syncs.yaml
starsync sync gists     --config docs/example-syncs.yaml
starsync sync following --config docs/example-syncs.yaml
```

每个 subcommand 只处理配置文件中对应 `resource` 的条目。

### 预演（强烈推荐先跑一次）

```bash
starsync sync stars     --from xsharp --to justdn --dry-run
starsync sync gists     --from xsharp --to justdn --dry-run
starsync sync following --from xsharp --to justdn --dry-run
```

### 镜像模式（破坏性：会取消目标账号上源账号没有的条目）

```bash
# 默认会列出将被取消的清单并要求输入 yes 确认
starsync sync stars     --from xsharp --to justdn --mirror
starsync sync following --from xsharp --to justdn --mirror

# 非交互场景跳过确认
starsync sync stars --from xsharp --to justdn --mirror --yes
```

### 写操作并发（`--concurrency`）

`--concurrency` 控制写操作（star / unstar / create gist / follow 等）的并发度。**默认 1（串行，最稳）**。同步几百上千个条目时建议调高：

```bash
# 并发 4
starsync sync stars     --from xsharp --to justdn --concurrency 4
starsync sync gists     --from xsharp --to justdn --concurrency 4
starsync sync following --from xsharp --to justdn --concurrency 4
```

> ⚠️ **GitHub secondary rate limit**：并发过大会触发 429/403 限流反 ban。建议 **≤ 8**。已被限流时 `github.Client.do` 会自动读 `Retry-After` 退避重试。

### 其它命令

```bash
starsync doctor    # 检查 gh 登录状态与账号可用性
starsync version   # 输出版本号（含 git commit 和构建时间）
```

## 发布流程

- 提交到 `main` → CI 自动跑 vet / test（带 -race）/ build
- 维护者本地打 tag 并 push：

  ```bash
  git tag v0.1.0
  git push --tags
  ```

- Tag push 触发 `.github/workflows/release.yml`，GoReleaser 自动：
  - 跨平台构建 6 个二进制（linux/darwin/windows × amd64/arm64）
  - 打包为 tar.gz（unix）和 zip（windows）
  - 生成 SHA256 checksums
  - 根据 conventional commits 自动分组 changelog
  - 创建 GitHub Release 并附所有产物

本地验证 release 流程（不发布）：

```bash
goreleaser release --snapshot --clean
```

## 资源类型

| 命令 | Item 标识 | 同步语义 |
|---|---|---|
| `sync stars` | `owner/repo` | 在目标账号 star 该仓库（标记型） |
| `sync gists` | gist 的 `description` | 在目标账号创建/删除内容相同的 gist（内容复制型）；**空 description 的 gist 跳过**，不同步 |
| `sync following` | GitHub `username` | 在目标账号关注/取关该用户（标记型） |

## Flags（`sync <resource>`）

| flag | 默认 | 说明 |
|---|---|---|
| `--from` | — | 源账号（gh 已登录的用户名）；用 `--config` 时不需要 |
| `--to` | — | 目标账号，可重复传多个 |
| `--config` | — | 多对多映射配置文件路径；与 `--from/--to` 互斥 |
| `--mirror` | false | 镜像模式（含删除，破坏性） |
| `--dry-run` | false | 只预演不写入 |
| `--json` | false | stdout 输出结构化 JSON Report |
| `--yes` | false | 跳过所有交互确认（非交互 / Agent） |
| `--concurrency` | 1 | 写操作并发数；建议 ≤ 8 避免 secondary rate limit |

## 退出码

| 码 | 含义 |
|---|---|
| `0` | 全部成功 |
| `1` | 运行级错误（参数错误、gh 未登录、拉取列表失败等） |
| `2` | 部分条目失败（如某些私有 repo 无访问权），详见 Report 的 `failed[]` |

## JSON 输出（`--json`）

stdout 输出 Report 数组，每个 `source → target` 一项：

```json
[
  {
    "resource": "stars",
    "from": "xsharp",
    "to": "justdn",
    "mode": "union",
    "dry_run": true,
    "source_count": 1779,
    "target_before": 1046,
    "added": ["owner/repo", "..."],
    "removed": [],
    "skipped_already": 203,
    "failed": [],
    "counts": { "added": 1576, "removed": 0, "failed": 0 }
  }
]
```

## 安全说明

- 本工具**不写入、不缓存任何 token**；凭据只在内存中短暂持有，来源为 `gh` 的本机 keyring。
- 配置文件只描述账号映射关系，**不含任何凭据**。

## 设计文档

- 需求与设计：[`docs/requirements.md`](docs/requirements.md)
- 实现计划：[`docs/implementation-plan.md`](docs/implementation-plan.md)
