import Foundation

public enum AppPaths {
    public static let bundleID = "com.simon.youtubereader"
    public static let appGroup = "group.com.simon.youtubereader"

    public static var localRoot: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return base.appendingPathComponent("YouTubeReader", isDirectory: true)
    }
    public static var database: URL { localRoot.appendingPathComponent("videos.sqlite") }
    public static var settings: URL { localRoot.appendingPathComponent("settings.json") }
    public static var lock: URL {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("YouTubeReaderLocalLock/.youtube-reader.lock")
    }
    public static var defaultSyncFile: URL { localRoot.appendingPathComponent("youtube-reader-sync.json") }
    public static var defaults: UserDefaults { UserDefaults(suiteName: appGroup) ?? .standard }
}

public struct LocalSettings: Codable, Sendable {
    public var apiKey: String = ""
    public var refreshMinutes: Int = 60
    public var syncFilePath: String = AppPaths.defaultSyncFile.path
    public var favoritesBackupPath: String = AppPaths.localRoot.appendingPathComponent("favorites-backup.json").path
    public var showShorts: Bool = false
    public var notificationChannelIDs: [String] = []
    public var lastRefresh: Date?
    public var nextRefresh: Date?
    public var recentFailures: [RefreshFailure] = []

    public init(apiKey: String = "", refreshMinutes: Int = 60, syncFilePath: String? = nil,
                favoritesBackupPath: String? = nil, showShorts: Bool = false,
                notificationChannelIDs: [String] = [], lastRefresh: Date? = nil,
                nextRefresh: Date? = nil, recentFailures: [RefreshFailure] = []) {
        self.apiKey = apiKey
        self.refreshMinutes = refreshMinutes
        self.syncFilePath = syncFilePath ?? AppPaths.defaultSyncFile.path
        self.favoritesBackupPath = favoritesBackupPath ?? AppPaths.localRoot.appendingPathComponent("favorites-backup.json").path
        self.showShorts = showShorts
        self.notificationChannelIDs = notificationChannelIDs
        self.lastRefresh = lastRefresh
        self.nextRefresh = nextRefresh
        self.recentFailures = recentFailures
    }

    public static func load() -> LocalSettings {
        guard let data = try? Data(contentsOf: AppPaths.settings),
              let settings = try? JSONDecoder().decode(LocalSettings.self, from: data) else { return .init() }
        return settings
    }

    public func save() throws {
        try FileManager.default.createDirectory(at: AppPaths.localRoot, withIntermediateDirectories: true)
        let data = try JSONEncoder().encode(self)
        try data.write(to: AppPaths.settings, options: .atomic)
    }
}
