# Pebble Android 客户端 (`apps/android`)

纯原生 Kotlin + Jetpack Compose + Material 3 Expressive + Material Symbols Rounded 客户端，包名 `ai.pebble.android`（Debug 变体为 `ai.pebble.android.debug`），一套代码自适应手机窄屏与平板/折叠屏宽屏，视觉与交互规范见仓库根目录 `DESIGN.md`。

- 支持 `pebble://` 与 `bothub://` Deep Link（`connect`、`bot/{id}`、`desktop?target=`、`interrupt/{id}`）。
- 扫码配对采用 CameraX + ML Kit Barcode Scanning Bundled 离线版（零 Google Play 服务依赖），支持双指缩放、点击对焦、手电筒与相册选图识码。
- 隧道与远程桌面硬解复用 `cmd/libbothub` 生成的 `libbothub.so` 与原生 `MediaCodec` + `TextureView`。
- 保存的主机与令牌使用 Android Keystore (`AES/GCM/NoPadding`) 加密存储。

## 构建

先在仓库根目录构建 Android 原生隧道库（生成 `arm64-v8a` 与 `x86_64` 切片并同步至 `app/src/main/jniLibs`）：

```bash
make android-tunnel
```

构建并运行单元测试与 Release APK：

```bash
cd apps/android
./gradlew :app:testDebugUnitTest :app:assembleRelease
```

或在仓库根目录直接运行：

```bash
make android-native
```

Release APK 产物位于 `apps/android/app/build/outputs/apk/release/app-release.apk`。
