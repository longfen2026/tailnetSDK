// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

import Foundation

public extension URLSession {
    /// Creates a URLSession configured to route all traffic through the TailnetClient's local SOCKS5 proxy.
    ///
    /// Usage:
    /// ```swift
    /// let session = URLSession.tailnetSession(client: client)
    /// let (data, _) = try await session.data(from: URL(string: "http://my-peer.tailnet:8080/api")!)
    /// ```
    static func tailnetSession(client: TailnetClient, configuration: URLSessionConfiguration = .default) -> URLSession {
        guard let proxy = client.socks5Proxy() else {
            return URLSession(configuration: configuration)
        }

        let config = configuration
        config.connectionProxyDictionary = [
            kCFStreamPropertySOCKSProxyHost as String: proxy.host,
            kCFStreamPropertySOCKSProxyPort as String: proxy.port,
            kCFStreamPropertySOCKSVersion as String: kCFStreamSocketSOCKSVersion5
        ]
        return URLSession(configuration: config)
    }
}
