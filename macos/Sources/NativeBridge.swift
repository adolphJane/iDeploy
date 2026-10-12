import Foundation
import Darwin

struct J: Identifiable {
    let raw: Any

    init(_ raw: Any?) { self.raw = raw ?? NSNull() }
    subscript(_ key: String) -> J { J((raw as? [String: Any])?[key]) }
    subscript(_ index: Int) -> J {
        let values = raw as? [Any] ?? []
        return J(values.indices.contains(index) ? values[index] : nil)
    }
    var id: String { self["id"].string }
    var string: String {
        if let text = raw as? String { return text }
        if let number = raw as? NSNumber { return number.stringValue }
        return ""
    }
    var int: Int { (raw as? NSNumber)?.intValue ?? Int(string) ?? 0 }
    var bool: Bool { (raw as? NSNumber)?.boolValue ?? false }
    var array: [J] { (raw as? [Any] ?? []).map(J.init) }
    var object: [String: Any] { raw as? [String: Any] ?? [:] }
    var isNull: Bool { raw is NSNull }
}

struct BridgeError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

enum NativeBridge {
    static let socketPath = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent("Library/Application Support/iDeploy/control.sock").path

    static func call(_ name: String, _ arguments: [Any] = []) async throws -> J {
        try await Task.detached(priority: .userInitiated) {
            try request(name, arguments)
        }.value
    }

    private static func request(_ name: String, _ arguments: [Any]) throws -> J {
        let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw BridgeError(message: "无法创建本机连接") }
        defer { Darwin.close(fd) }

        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(socketPath.utf8CString)
        guard bytes.count <= MemoryLayout.size(ofValue: address.sun_path) else {
            throw BridgeError(message: "本机连接路径过长")
        }
        withUnsafeMutableBytes(of: &address.sun_path) { destination in
            destination.copyBytes(from: bytes.map { UInt8(bitPattern: $0) })
        }
        let connected = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard connected == 0 else {
            throw BridgeError(message: "无法连接 iDeploy 后台服务")
        }
        var timeout = timeval(tv_sec: 300, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
        let payload: [String: Any] = [
            "command": "native.call",
            "nativeMethod": name,
            "nativeArgs": arguments
        ]
        var data = try JSONSerialization.data(withJSONObject: payload)
        data.append(0x0A)
        try data.withUnsafeBytes { buffer in
            guard let base = buffer.baseAddress else { return }
            var offset = 0
            while offset < buffer.count {
                let count = Darwin.write(fd, base.advanced(by: offset), buffer.count - offset)
                guard count > 0 else { throw BridgeError(message: "发送请求失败") }
                offset += count
            }
        }
        var response = Data()
        var chunk = [UInt8](repeating: 0, count: 8192)
        while response.count < 16 * 1024 * 1024 {
            let count = Darwin.read(fd, &chunk, chunk.count)
            guard count > 0 else { throw BridgeError(message: "后台服务没有响应") }
            response.append(contentsOf: chunk.prefix(count))
            if chunk.prefix(count).contains(0x0A) { break }
        }
        guard let envelope = try JSONSerialization.jsonObject(with: response) as? [String: Any] else {
            throw BridgeError(message: "后台响应格式无效")
        }
        if (envelope["ok"] as? Bool) != true {
            throw BridgeError(message: envelope["error"] as? String ?? "操作失败")
        }
        return J(envelope["data"])
    }
}
