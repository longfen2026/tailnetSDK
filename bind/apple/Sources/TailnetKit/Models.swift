// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

import Foundation

public enum TailnetError: LocalizedError {
    case invalidHandle
    case invalidArgument(String)
    case backendError(String)
    case timeout
    case closed

    public var errorDescription: String? {
        switch self {
        case .invalidHandle:
            return "Tailnet handle is invalid or expired."
        case .invalidArgument(let msg):
            return "Invalid argument: \(msg)"
        case .backendError(let msg):
            return "Tailnet backend error: \(msg)"
        case .timeout:
            return "Operation timed out."
        case .closed:
            return "Node has been closed."
        }
    }
}

public struct TailnetPeer: Codable, Sendable {
    public let id: String
    public let name: String
    public let dnsName: String
    public let tailscaleIPs: [String]
    public let online: Bool
    public let os: String?

    enum CodingKeys: String, CodingKey {
        case id = "ID"
        case name = "Name"
        case dnsName = "DNSName"
        case tailscaleIPs = "TailscaleIPs"
        case online = "Online"
        case os = "OS"
    }
}

public struct TailnetStatus: Codable, Sendable {
    public let state: String
    public let tailscaleIPs: [String]
    public let magicDNSSuffix: String
    public let peers: [TailnetPeer]

    public var isRunning: Bool {
        return state == "Running"
    }

    enum CodingKeys: String, CodingKey {
        case state = "State"
        case tailscaleIPs = "TailscaleIPs"
        case magicDNSSuffix = "MagicDNSSuffix"
        case peers = "Peers"
    }
}

public struct TailnetIdentity: Codable, Sendable {
    public let nodeName: String
    public let userLogin: String
    public let displayName: String

    enum CodingKeys: String, CodingKey {
        case nodeName = "NodeName"
        case userLogin = "UserLogin"
        case displayName = "DisplayName"
    }
}

public struct TailnetEvent: Codable, Sendable {
    public let kind: String
    public let message: String
    public let state: String?

    enum CodingKeys: String, CodingKey {
        case kind = "Kind"
        case message = "Message"
        case state = "State"
    }
}
