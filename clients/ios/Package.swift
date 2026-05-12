// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "MatrixclawClient",
    platforms: [
        .iOS(.v15),
        .macOS(.v12)
    ],
    products: [
        .library(
            name: "MatrixclawClient",
            targets: ["MatrixclawClient"]
        )
    ],
    targets: [
        .target(
            name: "MatrixclawClient"
        )
    ]
)
