import Foundation
import YoutubeReaderCore

/// Entry point for the SMAppService LaunchAgent helper. Refreshes SQLite and posts local
/// notifications even when the main app is closed. The app holds the shared-JSON flock while
/// open, so this helper waits for that lock before writing and never blocks inbound Syncthing
/// sync of the canonical file.
@main
enum YoutubeReaderHelper {
    static func main() async {
        let settings = LocalSettings.load()
        guard !settings.apiKey.isEmpty else { return }
        let db = Database(url: AppPaths.database)
        let deviceID = AppPaths.defaults.string(forKey: "deviceID") ?? {
            let id = UUID().uuidString
            AppPaths.defaults.set(id, forKey: "deviceID")
            return id
        }()
        let syncStore = SyncStore(fileURL: URL(fileURLWithPath: settings.syncFilePath), deviceID: deviceID)
        let lock = LocalFileLock(url: AppPaths.lock)
        let api = YouTubeAPI(key: settings.apiKey)
        let service = RefreshService(api: api, database: db, syncStore: syncStore, settings: settings)
        try? db.open()
        var state = (try? syncStore.load()) ?? SyncSettings(deviceID: deviceID, updatedAt: .distantPast, groups: [], channels: [], readStates: [], favorites: [])
        let channelIDs = state.channels.map(\.id)
        guard !channelIDs.isEmpty else { return }
        _ = await service.refresh(channelIDs: channelIDs)
        try? lock.lock()
        defer { lock.unlock() }
        state = (try? syncStore.load()) ?? state
        try? syncStore.save(state)
    }
}
