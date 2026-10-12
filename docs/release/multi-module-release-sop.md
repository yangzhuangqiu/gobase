# gobase Multi-Module Release SOP

适用场景：单仓库多 `go.mod`（包含 `config`、`log`、`ioc`、`app-runtime`、`app`、适配器与 `examples`）。

## 1. Core Rules

1. 每个模块独立 SemVer。
2. 每个模块独立打 tag，使用“模块前缀 tag”：
   - `config/vX.Y.Z`
   - `config-nacos/vX.Y.Z`
   - `config-consul/vX.Y.Z`
   - `app-runtime/vX.Y.Z`
   - `app/vX.Y.Z`
3. 发布前移除或确认本地 `replace` 不会进入外部发布版本。
4. 跨模块依赖按“底层先发、上层后发”顺序。

## 2. Directory Example

```text
gobase/
  config/         (module github.com/yangzhuangqiu/gobase/config)
  config-nacos/   (module github.com/yangzhuangqiu/gobase/config-nacos)
  config-consul/  (module github.com/yangzhuangqiu/gobase/config-consul)
  app-runtime/    (module github.com/yangzhuangqiu/gobase/app-runtime)
  app/            (module github.com/yangzhuangqiu/gobase/app)
```

## 3. Standard Release Flow

1. 在目标模块目录执行：
   - `test ./...`
   - `vet ./...`
2. 更新该模块文档/示例。
3. 创建并推送模块前缀 tag，例如：
   - `git tag config/v0.3.0`
   - `git push origin config/v0.3.0`
4. 在干净环境验证：
   - `go get github.com/yangzhuangqiu/gobase/config@v0.3.0`

## 4. Cross-Module Release Order

场景：`app` 依赖新的 `app-runtime`，而 `app-runtime` 依赖 `config/log/ioc`。

1. 发布 `config`、`log`、`ioc`。
2. 发布 `log-zap`，以及 `config-nacos` / `config-consul` 等 provider。
3. 在 `app-runtime/go.mod` 升级底层依赖并发布 `app-runtime`。
4. 发布依赖 `config/log/log-zap` 的 `app-log-zap`；它不要求同步升级或发布 `app-runtime`。
5. 发布 `app-observe-*` 等其他适配器。
6. 在 `app/go.mod` 升级 `app-runtime` 并发布 `app`。

## 5. Release Notes Template

```text
## config-nacos v0.1.0
- feat: ...
- fix: ...
- docs: ...

Compatibility:
- Breaking / Non-breaking

Upgrade:
- go get github.com/yangzhuangqiu/gobase/config-nacos@v0.1.0
```

## 6. Common Pitfalls

1. 只打根 tag（`vX.Y.Z`）导致子模块版本不可解析。
2. 提交了本地 `replace ../xxx`。
3. 底层模块未发布，先发布上层模块导致依赖不可拉取。
4. breaking 变更未升级主版本。
