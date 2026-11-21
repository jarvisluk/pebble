Pod::Spec.new do |s|
  s.name             = 'BothubTunnel'
  s.version          = '0.1.0'
  s.summary          = 'Tailcat WireGuard tunnel proxy for Bot Hub'
  s.description      = 'Embeds bot-hub Tailcat client tunnel for iOS.'
  s.license          = 'MIT'
  s.author           = 'Bot Hub'
  s.homepage         = 'https://github.com/bot-hub/bot-hub'
  s.platforms        = { :ios => '16.4' }
  s.swift_version    = '5.9'
  s.source           = { :path => '.' }
  s.static_framework = true

  s.dependency 'ExpoModulesCore'
  s.frameworks = 'CoreFoundation', 'Security'
  s.libraries = 'bothub', 'resolv'

  s.source_files = '*.{h,m,swift}'
  s.public_header_files = 'BotHubTunnelBridge.h'
  s.exclude_files = 'Frameworks/**'
  s.xcconfig = {
    'LIBRARY_SEARCH_PATHS[sdk=iphoneos*]' => '$(inherited) "$(PODS_ROOT)/../../modules/bothub-tunnel/ios/Frameworks/BotHubTunnel.xcframework/ios-arm64"',
    'LIBRARY_SEARCH_PATHS[sdk=iphonesimulator*]' => '$(inherited) "$(PODS_ROOT)/../../modules/bothub-tunnel/ios/Frameworks/BotHubTunnel.xcframework/ios-arm64_x86_64-simulator"'
  }
  s.pod_target_xcconfig = {
    'DEFINES_MODULE' => 'YES',
    'SWIFT_COMPILATION_MODE' => 'wholemodule'
  }
end
