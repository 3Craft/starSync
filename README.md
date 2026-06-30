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

## 构建

```bash
go build -o starsync ./cmd/starsync
```

## 用法

### 一对一

```bash
starsync sync stars --from xsharp --to justdn
```

### 一对多

```bash
starsync sync stars --from xsharp --to justdn --to trendcms
```

### 多对多 / 双向（配置文件）

```bash
starsync sync stars --config docs/example-syncs.yaml
```

### 预演（强烈推荐先跑一次）

```bash
starsync sync stars --from xsharp --to justdn --dry-run
```

### 镜像模式（破坏性：会 unstar 目标账号上源账号没有的仓库）

```bash
# 默认会列出将被取消的清单并要求输入 yes 确认
starsync sync stars --from xsharp --to justdn --mirror

# 非交互场景跳过确认
starsync sync stars --from xsharp --to justdn --mirror --yes
```

### 其它命令

```bash
starsync doctor    # 检查 gh 登录状态与账号可用性
starsync version   # 输出版本号
```

## Flags（`sync stars`）

| flag | 默认 | 说明 |
|---|---|---|
| `--from` | — | 源账号（gh 已登录的用户名）；用 `--config` 时不需要 |
| `--to` | — | 目标账号，可重复传多个 |
| `--config` | — | 多对多映射配置文件路径；与 `--from/--to` 互斥 |
| `--mirror` | false | 镜像模式（含 unstar，破坏性） |
| `--dry-run` | false | 只预演不写入 |
| `--json` | false | stdout 输出结构化 JSON Report |
| `--yes` | false | 跳过所有交互确认（非交互 / Agent） |

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
