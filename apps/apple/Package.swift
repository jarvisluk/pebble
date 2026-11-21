// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "BotHubApp",
    platforms: [
        .iOS(.v17),
        .macOS(.v14),
    ],
    products: [
        .executable(name: "BotHubApp", targets: ["BotHubApp"]),
    ],
    targets: [
        .executableTarget(
            name: "BotHubApp",
            path: "Sources/BotHubApp"
        ),
    ]
)
