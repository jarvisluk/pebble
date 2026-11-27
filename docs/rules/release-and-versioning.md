# 版本号管理与发版流程（Release Runbook）

> **触发条件**：当需要调整 Pebble 版本号、检查多端版本一致性、打发版 tag 或执行本地/CI 打包发布时阅读本文件。

## 1. 版本号规范（SemVer + 单调构建号）

全仓统一使用 **Semantic Versioning 2.0.0**（从 `0.1.0` 起步）与单调递增整数 `build`（从 `1` 起步），单一真源为根目录 [`version.json`](../../version.json)：

```json
{
  "version": "0.1.0",
  "build": 1
}
```

- **正式版**：`X.Y.Z`（例如 `0.2.0`、`1.0.0`），对应 Git Tag `vX.Y.Z`。
- **预发布通道**：`X.Y.Z-alpha.N`、`X.Y.Z-beta.N`、`X.Y.Z-rc.N`（例如 `0.2.0-rc.1`），对应 Git Tag `vX.Y.Z-rc.1`；发布到 GitHub Release 时自动标记为 `prerelease`。
- **Apple 适配规则**：App Store / TestFlight 不允许 `CFBundleShortVersionString` / `MARKETING_VERSION` 携带 `-rc.1` 等预发布后缀，因此 [`scripts/version.py`](../../scripts/version.py) 会自动将 `<base>`（`X.Y.Z`）写入 `MARKETING_VERSION` 与 `CFBundleShortVersionString`，并通过递增 `CURRENT_PROJECT_VERSION`（`build`）区分同一版本的不同构建包。

## 2. 单一真源同步目标

执行 `python3 scripts/version.py sync`（或 `make version-sync`）会自动同步以下文件，禁止手动分散修改：

| 目标文件 | 同步字段 | 来源字段 |
|---|---|---|
| [`internal/protocol/protocol.go`](../../internal/protocol/protocol.go) | `var AppVersion = "..."` | `version` |
| [`web/package.json`](../../web/package.json) & [`web/package-lock.json`](../../web/package-lock.json) | `version` | `version` |
| [`apps/apple/Config/Version.xcconfig`](../../apps/apple/Config/Version.xcconfig) | `MARKETING_VERSION` / `CURRENT_PROJECT_VERSION` | `base` / `build` |
| [`apps/android/version.properties`](../../apps/android/version.properties) | `VERSION_NAME` / `VERSION_CODE` | `version` / `build` |
| [`build/darwin/Info.plist`](../../build/darwin/Info.plist) | `CFBundleShortVersionString` / `CFBundleVersion` | `base` / `build` |

> [`apps/apple/Pebble.xcodeproj/project.pbxproj`](../../apps/apple/Pebble.xcodeproj/project.pbxproj) 已移除各 target 的硬编码版本覆盖，统一继承 `Config/Signing.xcconfig` 引入的 `Config/Version.xcconfig`。`make version-check` 会自动校验是否出现漂移或残留硬编码。

## 3. 日常版本命令

```bash
# 查看当前版本与构建号
make version
python3 scripts/version.py show --json

# 校验所有端版本是否与 version.json 一致（CI 必检）
make version-check

# 仅同步 version.json 到各端文件
make version-sync

# 预览升级结果与 Changelog（不修改文件）
./scripts/release/prepare.sh minor --dry-run
```

## 4. 发版标准流程

### 4.1 在 `main` 分支准备发布提交与 Tag

在工作区干净的 `main` 分支运行：

```bash
# 常规版本发布（patch / minor / major）
make release-prepare BUMP=minor

# 预发布通道（默认基于下一个 minor 生成 X.Y.0-rc.1；可通过 RELEASE_ARGS="--base patch" 覆盖）
make release-prepare BUMP=rc

# 同一渠道递增预发布序号（rc.1 -> rc.2）或升级通道（alpha -> beta -> rc）
make release-prepare BUMP=rc
make release-prepare BUMP=patch   # 从 0.2.0-rc.2 毕业为正式版 0.2.0

# 仅递增 build 号（例如热修复重传商店）
make release-prepare BUMP=build
```

`prepare.sh` 会自动完成：工作区洁净检查 → `version.py check` → 更新 `version.json` 与所有目标文件 → 基于 Conventional Commits 更新 `CHANGELOG.md` → 创建 `chore(release): vX.Y.Z` 提交 → 创建附注 tag `vX.Y.Z`。

确认无误后推送分支与 tag，即可触发 `.github/workflows/release.yml` 自动构建并发布 GitHub Release：

```bash
git push origin main v0.2.0
```

### 4.2 发版产物矩阵

| 产物 | 生成入口 | 说明 |
|---|---|---|
| `pebble_<ver>_linux_amd64.tar.gz`<br>`pebble_<ver>_linux_arm64.tar.gz`<br>`pebble_<ver>_darwin_arm64.tar.gz`<br>`pebble_<ver>_darwin_amd64.tar.gz` | `make release-cli` | 独立 `pebble` CLI / Server（内嵌 Web UI，支持 `pebble version [--json]`） |
| `Pebble_<ver>_macos_<arch>.dmg`<br>`Pebble_<ver>_macos_<arch>.zip` | `make release-macos` | macOS 原生 `Pebble.app`（内置 `pebble` sidecar、`agent-desktop`、`agent-browser`） |
| `Pebble_<ver>_android.apk` | `make release-android` | Android Release APK（含 `libbothub.so` 隧道库与单测门禁） |
| `SHA256SUMS.txt` | `make release-checksums` | 全量发布产物 SHA-256 校验清单 |
| `Pebble-iOS-<ver>.xcarchive` | `make release-ios` | 仅本地 Xcode Archive / TestFlight 分发（不向 GitHub Release 传未签名 IPA） |

> 注：`windows/amd64` 独立 CLI 暂未纳入默认发布矩阵，因当前 `internal/harness` 仍依赖 Unix 进程组系统调用（`Setpgid` / `syscall.Kill`）。待进程管理层跨平台抽象完成后，可在 `PLATFORMS` 中补充 `windows/amd64`。

### 4.3 本地兜底发布 & iOS TestFlight

- **本地打包并直接通过 `gh` 发布 GitHub Release**：
  ```bash
  make release-cli
  make release-macos
  make release-android
  ./scripts/release/publish-local.sh --draft
  ```
- **iOS / iPadOS 本地 Archive / 上传 TestFlight**（需先配置 `apps/apple/Config/Signing.local.xcconfig`）：
  ```bash
  make release-ios                      # 仅生成 .xcarchive
  ./scripts/release/archive-ios.sh --export   # 导出 App Store / TestFlight IPA
  ./scripts/release/archive-ios.sh --upload   # 直接上传 App Store Connect
  ```

## 5. GitHub Secrets 正式签名配置（未配置时优雅降级）

未配置以下 Secrets 时，Release 工作流仍会完整产出所有安装包（macOS 回退为 ad-hoc 签名、Android 回退为 debug 签名），并在日志中明确提示；配置后即自动启用正式签名与 Apple 公证：

| Secret 名称 | 用途 |
|---|---|
| `APPLE_DEVELOPER_ID_CERT_P12` | Base64 编码的 `Developer ID Application` `.p12` 证书 |
| `APPLE_DEVELOPER_ID_CERT_PASSWORD` | `.p12` 证书导出密码 |
| `APPLE_NOTARY_KEY_BASE64` / `APPLE_NOTARY_KEY_ID` / `APPLE_NOTARY_ISSUER_ID` | App Store Connect API Key（推荐用于 macOS 公证） |
| `APPLE_NOTARY_APPLE_ID` / `APPLE_NOTARY_TEAM_ID` / `APPLE_NOTARY_PASSWORD` | Apple ID App 专用密码方式公证（备选） |
| `ANDROID_KEYSTORE_BASE64` | Base64 编码的 Android Release `.jks` / `.keystore` |
| `ANDROID_KEYSTORE_PASSWORD` / `ANDROID_KEY_ALIAS` / `ANDROID_KEY_PASSWORD` | Android Keystore 密码与密钥别名 |
