import Foundation
import SQLite3

private let SQLITE_TRANSIENT = unsafeBitCast(-1 as Int, to: sqlite3_destructor_type.self)

public final class Database {
    public let url: URL
    private var handle: OpaquePointer?

    public init(url: URL) { self.url = url }

    public func open() throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        guard sqlite3_open(url.path, &handle) == SQLITE_OK else { throw NSError(domain: "sqlite", code: 1) }
        try exec("""
        PRAGMA journal_mode=WAL;
        CREATE TABLE IF NOT EXISTS videos (
          id TEXT PRIMARY KEY, channel_id TEXT NOT NULL, channel_title TEXT NOT NULL,
          title TEXT NOT NULL, description TEXT NOT NULL, thumbnail TEXT NOT NULL,
          avatar TEXT, published REAL NOT NULL, duration REAL, is_short INTEGER NOT NULL,
          created REAL NOT NULL);
        CREATE TABLE IF NOT EXISTS channels (
          id TEXT PRIMARY KEY, title TEXT NOT NULL, avatar TEXT, uploads_playlist TEXT, uploaded_at REAL);
        CREATE TABLE IF NOT EXISTS refreshes (key TEXT PRIMARY KEY, value TEXT NOT NULL);
        CREATE INDEX IF NOT EXISTS idx_videos_published ON videos(published);
        """)
    }

    private func exec(_ sql: String) throws {
        var error: UnsafeMutablePointer<CChar>?
        if sqlite3_exec(handle, sql, nil, nil, &error) != SQLITE_OK {
            let message = error.map { String(cString: $0) } ?? "unknown"
            sqlite3_free(error)
            throw NSError(domain: "sqlite", code: 2, userInfo: [NSLocalizedDescriptionKey: message])
        }
    }

    private func prepare(_ sql: String) throws -> OpaquePointer? {
        var stmt: OpaquePointer?
        guard sqlite3_prepare_v2(handle, sql, -1, &stmt, nil) == SQLITE_OK else {
            throw NSError(domain: "sqlite", code: 3, userInfo: [NSLocalizedDescriptionKey: String(cString: sqlite3_errmsg(handle))])
        }
        return stmt
    }

    public func channels() throws -> [Channel] {
        let stmt = try prepare("SELECT id, title, avatar, uploaded_at FROM channels")
        defer { sqlite3_finalize(stmt) }
        var result: [Channel] = []
        while sqlite3_step(stmt) == SQLITE_ROW {
            let id = String(cString: sqlite3_column_text(stmt, 0))
            let title = String(cString: sqlite3_column_text(stmt, 1))
            let avatar = sqlite3_column_text(stmt, 2).map { String(cString: $0) }
            let uploaded = sqlite3_column_double(stmt, 3)
            result.append(Channel(id: id, title: title, groupIDs: [], avatarURL: avatar, uploadedAt: uploaded > 0 ? Date(timeIntervalSince1970: uploaded) : nil))
        }
        return result
    }

    public func setChannelUploadedAt(_ id: String, date: Date, playlist: String?) throws {
        let stmt = try prepare("UPDATE channels SET uploaded_at = ?, uploads_playlist = COALESCE(?, uploads_playlist) WHERE id = ?")
        defer { sqlite3_finalize(stmt) }
        sqlite3_bind_double(stmt, 1, date.timeIntervalSince1970)
        if let playlist { sqlite3_bind_text(stmt, 2, playlist, -1, SQLITE_TRANSIENT) } else { sqlite3_bind_null(stmt, 2) }
        sqlite3_bind_text(stmt, 3, id, -1, SQLITE_TRANSIENT)
        sqlite3_step(stmt)
    }

    public func setChannelAvatar(_ id: String, url: String?) throws {
        let stmt = try prepare("UPDATE channels SET avatar = ? WHERE id = ?")
        defer { sqlite3_finalize(stmt) }
        if let url { sqlite3_bind_text(stmt, 1, url, -1, SQLITE_TRANSIENT) } else { sqlite3_bind_null(stmt, 1) }
        sqlite3_bind_text(stmt, 2, id, -1, SQLITE_TRANSIENT)
        sqlite3_step(stmt)
    }

    public func insertChannels(_ channels: [Channel]) throws {
        try exec("BEGIN")
        do {
            let stmt = try prepare("INSERT OR IGNORE INTO channels (id, title) VALUES (?, ?)")
            defer { sqlite3_finalize(stmt) }
            for channel in channels {
                sqlite3_bind_text(stmt, 1, channel.id, -1, SQLITE_TRANSIENT)
                sqlite3_bind_text(stmt, 2, channel.title, -1, SQLITE_TRANSIENT)
                sqlite3_step(stmt)
                sqlite3_reset(stmt)
                sqlite3_clear_bindings(stmt)
            }
            try exec("COMMIT")
        } catch { try? exec("ROLLBACK"); throw error }
    }

    public func videos() throws -> [Video] {
        let stmt = try prepare("SELECT id, channel_id, channel_title, title, description, thumbnail, avatar, published, duration, is_short, created FROM videos")
        defer { sqlite3_finalize(stmt) }
        var result: [Video] = []
        while sqlite3_step(stmt) == SQLITE_ROW {
            let duration = sqlite3_column_double(stmt, 8)
            result.append(Video(
                id: String(cString: sqlite3_column_text(stmt, 0)),
                channelID: String(cString: sqlite3_column_text(stmt, 1)),
                channelTitle: String(cString: sqlite3_column_text(stmt, 2)),
                title: String(cString: sqlite3_column_text(stmt, 3)),
                textDescription: String(cString: sqlite3_column_text(stmt, 4)),
                thumbnailURL: String(cString: sqlite3_column_text(stmt, 5)),
                avatarURL: sqlite3_column_text(stmt, 6).map { String(cString: $0) },
                publishedAt: Date(timeIntervalSince1970: sqlite3_column_double(stmt, 7)),
                duration: duration > 0 ? duration : nil,
                isShort: sqlite3_column_int(stmt, 9) != 0,
                createdAt: Date(timeIntervalSince1970: sqlite3_column_double(stmt, 10))
            ))
        }
        return result
    }

    public func upsertVideos(_ videos: [Video]) throws {
        try exec("BEGIN")
        do {
            let stmt = try prepare("""
            INSERT INTO videos (id, channel_id, channel_title, title, description, thumbnail, avatar, published, duration, is_short, created)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET channel_title=excluded.channel_title, title=excluded.title,
              description=excluded.description, thumbnail=excluded.thumbnail, avatar=COALESCE(excluded.avatar, videos.avatar),
              published=excluded.published, duration=COALESCE(excluded.duration, videos.duration),
              is_short=excluded.is_short
            """)
            defer { sqlite3_finalize(stmt) }
            for video in videos {
                bindText(stmt, 1, video.id); bindText(stmt, 2, video.channelID)
                bindText(stmt, 3, video.channelTitle); bindText(stmt, 4, video.title)
                bindText(stmt, 5, video.textDescription); bindText(stmt, 6, video.thumbnailURL)
                if let avatar = video.avatarURL { bindText(stmt, 7, avatar) } else { sqlite3_bind_null(stmt, 7) }
                sqlite3_bind_double(stmt, 8, video.publishedAt.timeIntervalSince1970)
                if let d = video.duration { sqlite3_bind_double(stmt, 9, d) } else { sqlite3_bind_null(stmt, 9) }
                sqlite3_bind_int(stmt, 10, video.isShort ? 1 : 0)
                sqlite3_bind_double(stmt, 11, video.createdAt.timeIntervalSince1970)
                sqlite3_step(stmt); sqlite3_reset(stmt); sqlite3_clear_bindings(stmt)
            }
            try exec("COMMIT")
        } catch { try? exec("ROLLBACK"); throw error }
    }

    public func deleteVideos(ids: [String]) throws {
        guard !ids.isEmpty else { return }
        try exec("BEGIN")
        do {
            let stmt = try prepare("DELETE FROM videos WHERE id = ?")
            defer { sqlite3_finalize(stmt) }
            for id in ids {
                bindText(stmt, 1, id); sqlite3_step(stmt); sqlite3_reset(stmt); sqlite3_clear_bindings(stmt)
            }
            try exec("COMMIT")
        } catch { try? exec("ROLLBACK"); throw error }
    }

    public func value(key: String) -> String? {
        guard let stmt = try? prepare("SELECT value FROM refreshes WHERE key = ?") else { return nil }
        defer { sqlite3_finalize(stmt) }
        bindText(stmt, 1, key)
        guard sqlite3_step(stmt) == SQLITE_ROW else { return nil }
        return String(cString: sqlite3_column_text(stmt, 0))
    }

    public func setValue(_ value: String, key: String) throws {
        let stmt = try prepare("INSERT INTO refreshes (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value")
        defer { sqlite3_finalize(stmt) }
        bindText(stmt, 1, key); bindText(stmt, 2, value); sqlite3_step(stmt)
    }

    private func bindText(_ stmt: OpaquePointer?, _ index: Int32, _ value: String) {
        sqlite3_bind_text(stmt, index, value, -1, SQLITE_TRANSIENT)
    }

    deinit { sqlite3_close(handle) }
}
