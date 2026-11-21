# Bot Hub Mobile (`apps/mobile`)

跨平台原生移动端应用（支持 iOS 与 Android，兼顾 Web 调试预览），基于 **Expo SDK 58**、**Expo Router** 与本地原生模块 `modules/bothub-tunnel`（封装仓库内 `cmd/libbothub` Go Tailcat 客户端核心，无需安装或切换系统 VPN）。

## 前置构建（Tailcat 原生隧道核心）

从仓库根目录运行：

```bash
# 1. 构建 iOS 真机与模拟器 XCFramework -> build/mobile/ios/BotHubTunnel.xcframework
make ios-tunnel

# 2. 构建 Android JNI 动态库 -> build/mobile/android/jniLibs/{arm64-v8a,x86_64}/libbothub.so
#    （需已设置 ANDROID_NDK_HOME 或 ANDROID_HOME/ndk）
make android-tunnel
```

## 运行开发构建

> Expo Go 不包含自定义的 `BothubTunnel` 原生模块，扫码连接 `tailcat://` 需要运行开发构建（`expo run:ios` / `expo run:android`）。

```bash
cd apps/mobile
npm install

# iOS 真机或模拟器
npx expo run:ios

# Android 真机或模拟器
npx expo run:android
```

## 配对方式

1. 在主电脑的 Bot Hub 桌面端打开「远程访问」，开启 Tailcat 远程访问并确保已配置 `apiToken`。
2. 手机端打开 **Bot Hub → 连接主电脑 → 扫码连接**，扫描桌面端生成的二维码（格式 `tailcat://connect/<addr>?port=3789#token=<apiToken>`），即可自动提取口令并建立加密隧道直连。
3. 局域网调试也可直接填写 `http://<电脑局域网IP>:3789`（或粘贴带 `#token=...` 的地址）。
