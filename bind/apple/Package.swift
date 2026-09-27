// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "TailnetKit",
    platforms: [
        .macOS(.v12),
        .iOS(.v15)
    ],
    products: [
        .library(
            name: "TailnetKit",
            targets: ["TailnetKit"]
        ),
    ],
    targets: [
        .binaryTarget(
            name: "libtailnet",
            path: "../../dist/Tailnet.xcframework"
        ),
        .target(
            name: "TailnetKit",
            dependencies: ["libtailnet"],
            path: "Sources/TailnetKit"
        ),
    ]
)
