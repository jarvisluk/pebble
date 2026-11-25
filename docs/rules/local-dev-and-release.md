# 构建入口、隔离联调与合入 `main` 换包规范

> **触发条件**：当需要编译测试特定模块、启动本地服务做联调/性能基准，或将已验证变更合并入 `main` 分支时阅读本文件。

## 1. 按改动范围选择构建与单测入口

只构建和验证与当前任务直接相关的模块，不要为无关改动跑全端编译：

| 改动模块 | 构建与单测命令 |
|---|---|
| Go 后端 / CLI / 协议（`cmd/`、`internal/`） | `make build && make test` |
| Web 前端（`web/`） | 先 `make web`（产出 `internal/web/dist`），再按需 `make build`（或仅改 legacy 壳层时 `make app-wails`） |
| macOS 原生应用（`apps/apple`） | `make app`（本地快速验证可用 `make app-fast`） |
| iOS / iPadOS 原生应用（`apps/apple`） | 若改动隧道层先执行 `make ios-tunnel`；工程编译 `xcodebuild -project apps/apple/Pebble.xcodeproj -scheme Pebble -destination 'generic/platform=iOS Simulator' build` |
| Android 原生应用（`apps/android`） | `make android-native`（含 `make android-tunnel` + `:app:assembleRelease`）；单测与 Release 打包：`cd apps/android && ./gradlew :app:testDebugUnitTest :app:assembleRelease` |

## 2. 隔离运行实例（联调 / 性能基准 / 回归）

- 本地拉起 `pebble` 做联调、性能压测或视觉回归时，**必须在临时目录使用独立配置与非默认端口**，严禁占用或污染宿主机默认常驻端口 `3789` 与真实运行数据（`config/`、`state/`、`workspaces/`）。
- 验证完毕后，主动终止后台测试进程，删除临时目录并确认测试端口已释放。

## 3. 合并入 `main` 与按需换包重装

- **触发时机**：**仅在将已验证的提交合并入 `main` 分支，或用户明确要求更新本机运行包时执行**；普通 worktree 开发、阶段性提交或纯文档/脚本改动，不操作主仓库，也不重启本机常驻应用。
- **按受影响端重装**（必须在主仓库 `main` 分支构建，不能只在临时 worktree 内构建或忘记重启主仓库应用，确保实际运行的始终是最新包）：
  - **涉及 Go 后端（`cmd/`、`internal/`）、`web/` 或 macOS 端（`apps/apple`）**：若改了 `web/` 先执行 `make web`，再执行 `make build && make app`，完成本机 `Pebble.app` 的覆盖安装，并使用 `open -g` 平滑重启（不抢焦点）。
  - **涉及 Android 端（`apps/android`）**：重新构建最新 Release APK（`make android-native`），并通过 `adb install -r` 覆盖安装到已连接的调试真机。
  - **涉及 iOS / iPadOS 端（`apps/apple`）**：编译真机包并通过 `xcrun devicectl device install app` 覆盖安装到已配对/连接的 iOS 真机。

## 4. 版本号与对外发版流程

全仓统一 SemVer 版本管理（[`version.json`](../../version.json)）、Changelog 生成与多端 Release 打包/GitHub Actions 发布流程见 [版本号管理与发版流程](release-and-versioning.md)。
