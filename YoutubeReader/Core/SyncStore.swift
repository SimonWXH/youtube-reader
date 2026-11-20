import Foundation
import Darwin

public struct SyncSettings: Codable, Sendable {
    public var schemaVersion: Int
    public var deviceID: String
    public var updatedAt: Date
    public var groups: [ChannelGroupRecord]
    public var channels: [ChannelRecord]
    public var readStates: [ReadStateRecord]
    public var favorites: [FavoriteRecord]

    public init(schemaVersion: Int = 1, deviceID: String, updatedAt: Date, groups: [ChannelGroupRecord], channels: [ChannelRecord], readStates: [ReadStateRecord], favorites: [FavoriteRecord]) {
        self.schemaVersion = schemaVersion
        self.deviceID = deviceID
        self.updatedAt = updatedAt
        self.groups = groups
        self.channels = channels
        self.readStates = readStates
        self.favorites = favorites
    }
}

public struct ChannelGroupRecord: Codable, Hashable, Sendable {
    public let id: String
    public var name: String
    public var channelIDs: [String]
    public var order: Int
    public var updatedAt: Date

    public init(id: String, name: String, channelIDs: [String], order: Int = 0, updatedAt: Date) {
        self.id = id
        self.name = name
        self.channelIDs = channelIDs
        self.order = order
        self.updatedAt = updatedAt
    }
}

public struct ChannelRecord: Codable, Hashable, Sendable {
    public let id: String
    public var title: String
    public var groupIDs: [String]
    public var updatedAt: Date

    public init(id: String, title: String, groupIDs: [String], updatedAt: Date) {
        self.id = id
        self.title = title
        self.groupIDs = groupIDs
        self.updatedAt = updatedAt
    }
}

public struct ReadStateRecord: Codable, Hashable, Sendable {
    public let id: String // "deviceID:videoID" keeps per-device writes traceable
    public var videoID: String
    public var deviceID: String
    public var read: Bool
    public var readAt: Date

    public init(id: String, videoID: String, deviceID: String, read: Bool, readAt: Date) {
        self.id = id
        self.videoID = videoID
        self.deviceID = deviceID
        self.read = read
        self.readAt = readAt
    }
}

public struct FavoriteRecord: Codable, Hashable, Sendable {
    public let id: String // videoID
    public var video: Video?
    public var favorited: Bool
    public var favoritedAt: Date
    public var updatedAt: Date

    public init(id: String, video: Video?, favorited: Bool = true, favoritedAt: Date, updatedAt: Date) {
        self.id = id
        self.video = video
        self.favorited = favorited
        self.favoritedAt = favoritedAt
        self.updatedAt = updatedAt
    }
}

public enum SyncEngine {
    /// Pure merge. Group/favorite conflicts use last write wins; any read wins for read state.
    public static func merge(local: SyncSettings, remote: SyncSettings) -> SyncSettings {
        var groups: [String: ChannelGroupRecord] = [:]
        for item in local.groups + remote.groups {
            if let existing = groups[item.id], existing.updatedAt > item.updatedAt { continue }
            groups[item.id] = item
        }
        var channels: [String: ChannelRecord] = [:]
        for item in local.channels + remote.channels {
            if let existing = channels[item.id], existing.updatedAt > item.updatedAt { continue }
            channels[item.id] = item
        }
        var favorites: [String: FavoriteRecord] = [:]
        for item in local.favorites + remote.favorites {
            if let existing = favorites[item.id], existing.updatedAt > item.updatedAt { continue }
            favorites[item.id] = item
        }
        // Any device marking a video read wins. Unread cannot undo another device's read.
        var reads: [String: ReadStateRecord] = [:]
        for item in local.readStates + remote.readStates {
            if item.read {
                if let existing = reads[item.videoID], !existing.read || item.readAt < existing.readAt {
                    reads[item.videoID] = item
                } else if reads[item.videoID] == nil {
                    reads[item.videoID] = item
                }
            } else if reads[item.videoID] == nil {
                reads[item.videoID] = item
            }
        }
        return SyncSettings(
            deviceID: local.deviceID,
            updatedAt: max(local.updatedAt, remote.updatedAt),
            groups: groups.values.sorted { $0.order == $1.order ? $0.id < $1.id : $0.order < $1.order },
            channels: channels.values.sorted { $0.id < $1.id },
            readStates: reads.values.sorted { $0.id < $1.id },
            favorites: favorites.values.sorted { $0.id < $1.id }
        )
    }

    public static func readVideoIDs(_ settings: SyncSettings) -> Set<String> {
        Set(settings.readStates.filter(\.read).map(\.videoID))
    }

    public static func markRead(_ settings: SyncSettings, videoID: String, at: Date) -> SyncSettings {
        var copy = settings
        copy.readStates.removeAll { $0.videoID == videoID && $0.deviceID == settings.deviceID }
        copy.readStates.append(ReadStateRecord(id: "\(settings.deviceID):\(videoID)", videoID: videoID, deviceID: settings.deviceID, read: true, readAt: at))
        copy.updatedAt = max(copy.updatedAt, at)
        return copy
    }

    public static func setFavorite(_ settings: SyncSettings, video: Video, favorited: Bool, at: Date) -> SyncSettings {
        var copy = settings
        let existing = copy.favorites.first { $0.id == video.id }
        copy.favorites.removeAll { $0.id == video.id }
        copy.favorites.append(FavoriteRecord(
            id: video.id,
            video: favorited ? video : existing?.video,
            favorited: favorited,
            favoritedAt: existing?.favoritedAt ?? at,
            updatedAt: at
        ))
        copy.updatedAt = max(copy.updatedAt, at)
        return copy
    }

    public static func replaceGroups(_ settings: SyncSettings, groups: [ChannelGroup], channels: [Channel], at: Date) -> SyncSettings {
        var copy = settings
        copy.groups = groups.enumerated().map { index, group in
            ChannelGroupRecord(id: group.id, name: group.name, channelIDs: group.channelIDs, order: index, updatedAt: at)
        }
        copy.channels = channels.map { ChannelRecord(id: $0.id, title: $0.title, groupIDs: $0.groupIDs, updatedAt: at) }
        copy.updatedAt = max(copy.updatedAt, at)
        return copy
    }
}

/// One-file sync store. The SQLite DB and all local settings stay outside this file.
public final class SyncStore {
    public let fileURL: URL
    public let deviceID: String
    private let decoder = JSONDecoder()
    private let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.outputFormatting = [.prettyPrinted, .sortedKeys]
        e.dateEncodingStrategy = .iso8601
        return e
    }()

    public init(fileURL: URL, deviceID: String) {
        self.fileURL = fileURL
        self.deviceID = deviceID
        decoder.dateDecodingStrategy = .iso8601
    }

    public func empty() -> SyncSettings {
        SyncSettings(deviceID: deviceID, updatedAt: Date(timeIntervalSince1970: 0), groups: [], channels: [], readStates: [], favorites: [])
    }

    /// Read all canonical and Syncthing conflict copies, merging every one before any write.
    public func load() throws -> SyncSettings {
        var merged = empty()
        for url in candidateFiles() {
            guard let data = try? Data(contentsOf: url),
                  let settings = try? decoder.decode(SyncSettings.self, from: data) else { continue }
            merged = SyncEngine.merge(local: merged, remote: settings)
        }
        return merged
    }

    public func save(_ settings: SyncSettings) throws {
        let merged = SyncEngine.merge(local: try load(), remote: settings)
        try write(merged)
    }

    private func candidateFiles() -> [URL] {
        let dir = fileURL.deletingLastPathComponent()
        let base = fileURL.deletingPathExtension().lastPathComponent
        let files = (try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil)) ?? []
        let conflicts = files.filter { name in
            let n = name.lastPathComponent
            return n != fileURL.lastPathComponent &&
                   (n.hasPrefix(base + ".sync-conflict-") || (n.hasPrefix(base + "~") && n.hasSuffix(".json")))
        }
        return [fileURL] + conflicts.sorted { $0.lastPathComponent < $1.lastPathComponent }
    }

    private func write(_ settings: SyncSettings) throws {
        let fm = FileManager.default
        try fm.createDirectory(at: fileURL.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: nil)
        // Dot-prefixed temp name stays outside normal Syncthing watched naming; atomic rename publishes it.
        let temp = fileURL.deletingLastPathComponent()
            .appendingPathComponent(".__ytr_sync_\(UUID().uuidString).tmp")
        let data = try encoder.encode(settings)
        try data.write(to: temp, options: .atomic)
        let handle = try FileHandle(forWritingTo: temp)
        try handle.synchronize()
        try handle.close()
        if fm.fileExists(atPath: fileURL.path) {
            _ = try fm.replaceItemAt(fileURL, withItemAt: temp, backupItemName: nil, options: [])
        } else {
            try fm.moveItem(at: temp, to: fileURL)
        }
        // fsync the file itself before publishing; directory fsync via open() below.
        let fd = open(fileURL.deletingLastPathComponent().path, O_RDONLY)
        if fd >= 0 {
            if fsync(fd) != 0 { NSLog("directory fsync failed: %d", errno) }
            close(fd)
        }
    }
}

/// Kernel flock in a private local directory. No marker file in the Syncthing folder.
public final class LocalFileLock {
    public let url: URL
    private var fd: Int32 = -1

    public init(url: URL) { self.url = url }

    public var isLocked: Bool {
        let fd = open(url.path, O_RDWR | O_CREAT, mode_t(0o600))
        guard fd >= 0 else { return false }
        defer { close(fd) }
        let result = flock(fd, LOCK_EX | LOCK_NB)
        if result == 0 { flock(fd, LOCK_UN) }
        return result != 0
    }

    public func lock() throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        fd = open(url.path, O_RDWR | O_CREAT, mode_t(0o600))
        guard fd >= 0 else { throw NSError(domain: "lock", code: Int(errno)) }
        if flock(fd, LOCK_EX) != 0 { throw NSError(domain: "lock", code: Int(errno)) }
    }

    /// Force take over a stale lock: close our descriptor and reopen with LOCK_EX | LOCK_NB after
    /// verifying the previous owner process is gone. macOS flock releases on process exit.
    public func forceTakeOver() throws {
        if fd >= 0 { close(fd); fd = -1 }
        try lock()
    }

    public func unlock() {
        if fd >= 0 { flock(fd, LOCK_UN); close(fd); fd = -1 }
    }

    deinit { unlock() }
}
