// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "YoutubeReader",
    platforms: [.macOS(.v13)],
    products: [
        .library(name: "YoutubeReaderCore", targets: ["YoutubeReaderCore"])
    ],
    targets: [
        .target(name: "YoutubeReaderCore", path: "YoutubeReader/Core"),
        .executableTarget(name: "CoreChecks", dependencies: ["YoutubeReaderCore"], path: "Tests/YoutubeReaderTests/Checks")
    ]
)
