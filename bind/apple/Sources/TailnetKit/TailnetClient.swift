// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

import Foundation
import libtailnet

/// TailnetClient manages an embedded in-process Tailscale node for Apple platforms.
public final class TailnetClient: @unchecked Sendable {
    private let handle: Int32
    private let lock = NSLock()
    private var isClosed = false

    public init() {
        self.handle = tailnet_new()
    }

    deinit {
        close()
    }

    public func close() {
        lock.lock()
        defer lock.unlock()
        if !isClosed {
            isClosed = true
            _ = tailnet_close(handle)
        }
    }

    private func checkError(_ rc: Int32, op: String) throws {
        if rc == 0 { return }
        var buf = [CChar](repeating: 0, count: 512)
        _ = tailnet_errmsg(handle, &buf, buf.count)
        let msg = String(cString: buf)
        throw TailnetError.backendError("\(op) failed (\(rc)): \(msg.isEmpty ? "unknown" : msg)")
    }

    public func configure(dir: String, hostname: String = "", controlURL: String = "", authKey: String = "", ephemeral: Bool = false) throws {
        let dict: [String: Any] = [
            "dir": dir,
            "hostname": hostname,
            "controlURL": controlURL,
            "authKey": authKey,
            "ephemeral": ephemeral,
            "enableProxy": true
        ]
        let data = try JSONSerialization.data(withJSONObject: dict)
        guard let jsonStr = String(data: data, encoding: .utf8) else {
            throw TailnetError.invalidArgument("JSON encoding failed")
        }

        let rc = jsonStr.withCString { tailnet_configure(handle, UnsafeMutablePointer(mutating: $0)) }
        try checkError(rc, op: "configure")
    }

    public func start() throws {
        let rc = tailnet_start(handle)
        try checkError(rc, op: "start")
    }

    public func status() throws -> TailnetStatus {
        var cJson: UnsafeMutablePointer<CChar>? = nil
        let rc = tailnet_status_json(handle, &cJson)
        defer {
            if let ptr = cJson { tailnet_free(ptr) }
        }
        try checkError(rc, op: "status")

        guard let ptr = cJson, let jsonStr = String(cString: ptr).data(using: .utf8) else {
            throw TailnetError.backendError("status returned empty or invalid json")
        }
        return try JSONDecoder().decode(TailnetStatus.self, from: jsonStr)
    }

    public func authURL() throws -> URL? {
        var buf = [CChar](repeating: 0, count: 1024)
        let rc = tailnet_login_url(handle, &buf, buf.count)
        try checkError(rc, op: "login_url")
        let str = String(cString: buf).trimmingCharacters(in: .whitespacesAndNewlines)
        return str.isEmpty ? nil : URL(string: str)
    }

    public func startLoginInteractive() throws {
        let rc = tailnet_start_login_interactive(handle)
        try checkError(rc, op: "start_login_interactive")
    }

    public func logout() throws {
        let rc = tailnet_logout(handle)
        try checkError(rc, op: "logout")
    }

    public func socks5Proxy() -> (host: String, port: Int)? {
        var buf = [CChar](repeating: 0, count: 128)
        let rc = tailnet_proxy_socks5_addr(handle, &buf, buf.count)
        if rc != 0 { return nil }
        let str = String(cString: buf)
        let parts = str.split(separator: ":")
        guard parts.count == 2, let port = Int(parts[1]) else { return nil }
        return (String(parts[0]), port)
    }

    public func dial(network: String = "tcp", address: String, timeoutMS: Int = 30000) throws -> Int {
        var port: Int32 = 0
        let rc = network.withCString { cNet in
            address.withCString { cAddr in
                tailnet_dial(handle, cNet, cAddr, Int32(timeoutMS), &port)
            }
        }
        try checkError(rc, op: "dial")
        return Int(port)
    }

    public func whoIs(remoteAddr: String) throws -> TailnetIdentity {
        var cJson: UnsafeMutablePointer<CChar>? = nil
        let rc = remoteAddr.withCString { cAddr in
            tailnet_whois(handle, cAddr, &cJson)
        }
        defer {
            if let ptr = cJson { tailnet_free(ptr) }
        }
        try checkError(rc, op: "whois")

        guard let ptr = cJson, let data = String(cString: ptr).data(using: .utf8) else {
            throw TailnetError.backendError("whois returned empty JSON")
        }
        return try JSONDecoder().decode(TailnetIdentity.self, from: data)
    }
}
