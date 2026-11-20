import Foundation
import SwiftUI
import AppKit
import ServiceManagement
import UniformTypeIdentifiers
import YoutubeReaderCore

@MainActor
final class AppModel: ObservableObject {
    @Published var groups: [ChannelGroup] = []
    @Published var channels: [Channel] = []
    @Published var videos: [Video] = []
    @Published var sync: SyncSettings
    @Published var settings: LocalSettings
    @Published var selection: NavigationSelection = .all
    @Published var search = ""
    @Published var unreadOnly = false
    @Published var selectedVideo: Video?
    @Published var toast: String?
    @Published var isImporting = false
    @Published var isRefreshing = false
    @Published var isOffline = false
    @Published var showSettings = false

    private let database = Database(url: AppPaths.database)
    private var syncStore: SyncStore
    private let deviceID: String
    private let lock = LocalFileLock(url: AppPaths.lock)
    private let connectivity = ConnectivityMonitor()
    private var toastTask: Task<Void, Never>?

    init() {
        let settings = LocalSettings.load()
        let deviceID = AppPaths.defaults.string(forKey: "deviceID") ?? {
            let id = UUID().uuidString
            AppPaths.defaults.set(id, forKey: "deviceID")
            return id
        }()
        self.settings = settings
        self.deviceID = deviceID
        self.syncStore = SyncStore(fileURL: URL(fileURLWithPath: settings.syncFilePath), deviceID: deviceID)
        self.sync = (try? syncStore.load()) ?? SyncSettings(deviceID: deviceID, updatedAt: .distantPast, groups: [], channels: [], readStates: [], favorites: [])
        reload()
        try? lock.lock()
        if let last = settings.lastRefresh, settings.nextRefresh == nil {
            self.settings.nextRefresh = last.addingTimeInterval(Double(settings.refreshMinutes) * 60)
        }
        Notifier.requestAuthorization()
        registerBackgroundHelper()
        observeReachability()
    }

    deinit {
        lock.unlock()
    }

    func reload() {
        try? database.open()
        self.videos = (try? database.videos()) ?? []
        let dbChannels = (try? database.channels()) ?? []
        var merged: [Channel] = []
        for remote in sync.channels {
            var channel = dbChannels.first { $0.id == remote.id } ?? Channel(id: remote.id, title: remote.title, groupIDs: remote.groupIDs)
            channel.title = remote.title
            channel.groupIDs = remote.groupIDs
            merged.append(channel)
        }
        for channel in dbChannels where !merged.contains(where: { $0.id == channel.id }) {
            merged.append(channel)
        }
        self.channels = merged.sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
        self.groups = sync.groups.sorted { $0.order == $1.order ? $0.id < $1.id : $0.order < $1.order }
            .map { ChannelGroup(id: $0.id, name: $0.name, channelIDs: $0.channelIDs) }
    }

    var visibleVideos: [Video] {
        let read = SyncEngine.readVideoIDs(sync)
        var result = videos.filter { settings.showShorts || !$0.isShort }
        switch selection {
        case .all: break
        case .favorites:
            let favoriteRecords = sync.favorites.filter(\.favorited)
            let ids = Set(favoriteRecords.map(\.id))
            let hydrated = favoriteRecords.compactMap(\.video)
            result = (result.filter { ids.contains($0.id) } + hydrated.filter { !result.contains($0) })
        case .group(let id):
            let ids = Set(groups.first { $0.id == id }?.channelIDs ?? [])
            result = result.filter { ids.contains($0.channelID) }
        case .channel(let id):
            result = result.filter { $0.channelID == id }
        }
        if unreadOnly { result = result.filter { !read.contains($0.id) } }
        if !search.isEmpty {
            result = result.filter { $0.title.localizedCaseInsensitiveContains(search) || $0.channelTitle.localizedCaseInsensitiveContains(search) }
        }
        return VideoDeduplication.dedupe(result)
    }

    var title: String {
        switch selection {
        case .all: return "All Videos"
        case .favorites: return "Favorites"
        case .group(let id): return groups.first { $0.id == id }?.name ?? "Group"
        case .channel(let id): return channels.first { $0.id == id }?.title ?? "Channel"
        }
    }

    func isRead(_ video: Video) -> Bool { SyncEngine.readVideoIDs(sync).contains(video.id) }
    func isFavorite(_ video: Video) -> Bool { sync.favorites.contains { $0.id == video.id && $0.favorited } }

    func markRead(_ video: Video) {
        sync = SyncEngine.markRead(sync, videoID: video.id, at: Date())
        persistSync()
    }

    func toggleFavorite(_ video: Video) {
        sync = SyncEngine.setFavorite(sync, video: video, favorited: !isFavorite(video), at: Date())
        persistSync()
        exportFavoritesBackup()
    }

    func toggleNotification(_ channel: Channel) {
        let on = settings.notificationChannelIDs.contains(channel.id)
        settings.notificationChannelIDs = on ? settings.notificationChannelIDs.filter { $0 != channel.id } : settings.notificationChannelIDs + [channel.id]
        try? settings.save()
    }

    func enableNotifications(in group: ChannelGroup) {
        let ids = Set(group.channelIDs)
        settings.notificationChannelIDs = Array(Set(settings.notificationChannelIDs).union(ids))
        try? settings.save()
        showToast("Notifications enabled for \(group.name)")
    }

    func importOPML() {
        isImporting = true
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [UTType(filenameExtension: "opml") ?? .xml, .xml, .plainText]
        panel.allowsMultipleSelection = false
        panel.begin { [weak self] response in
            guard response == .OK, let url = panel.url else {
                Task { @MainActor in self?.isImporting = false }
                return
            }
            Task { @MainActor in
                guard let self else { return }
                defer { self.isImporting = false }
                do {
                    let result = try OPMLImporter.parse(data: Data(contentsOf: url))
                    let now = Date()
                    self.sync = SyncEngine.replaceGroups(self.sync, groups: result.groups, channels: result.channels, at: now)
                    try self.database.insertChannels(result.channels)
                    self.persistSync()
                    self.reload()
                    self.showToast("Imported \(result.groups.count) groups and \(result.channels.count) channels")
                } catch {
                    self.showToast("Import failed: \(error.localizedDescription)")
                }
            }
        }
    }

    func refresh(force: Bool = true) async {
        guard !settings.apiKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            showToast("Add an API key in Settings first")
            showSettings = true
            return
        }
        isRefreshing = true
        let api = YouTubeAPI(key: settings.apiKey)
        let service = RefreshService(api: api, database: database, syncStore: syncStore, settings: settings)
        let channelIDs = Array(Set(channels.map(\.id) + sync.channels.map(\.id)))
        let result = await service.refresh(channelIDs: channelIDs, force: force)
        isRefreshing = false
        settings = LocalSettings.load()
        sync = (try? syncStore.load()) ?? sync
        reload()
        if result.failures.isEmpty {
            showToast("Refresh complete")
        } else {
            showToast(result.failures.first ?? "Refresh failed")
        }
    }

    func forceTakeOverSync() {
        do {
            try lock.forceTakeOver()
            showToast("Sync lock taken over")
        } catch {
            showToast("Takeover failed: \(error.localizedDescription)")
        }
    }

    func saveSettings() {
        try? settings.save()
        let newStore = SyncStore(fileURL: URL(fileURLWithPath: settings.syncFilePath), deviceID: deviceID)
        syncStore = newStore
        sync = (try? newStore.load()) ?? sync
        reload()
        showToast("Settings saved")
    }

    func showToast(_ message: String) {
        toast = message
        toastTask?.cancel()
        toastTask = Task { @MainActor in
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            if !Task.isCancelled { toast = nil }
        }
    }

    private func persistSync() {
        try? syncStore.save(sync)
        sync = (try? syncStore.load()) ?? sync
    }

    private func exportFavoritesBackup() {
        guard let data = try? JSONEncoder().encode(sync.favorites) else { return }
        let url = URL(fileURLWithPath: settings.favoritesBackupPath)
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: url, options: .atomic)
    }

    private func registerBackgroundHelper() {
        let service = SMAppService.agent(plistName: "com.simon.youtubereader.helper.plist")
        guard service.status == .notRegistered else { return }
        do {
            try service.register()
        } catch {
            NSLog("Background helper registration failed: \(error.localizedDescription)")
        }
    }

    private func observeReachability() {
        NotificationCenter.default.addObserver(forName: .youtubeReaderConnectivityChanged, object: nil, queue: .main) { [weak self] note in
            let offline = note.userInfo?["offline"] as? Bool ?? false
            Task { @MainActor in
                self?.isOffline = offline
                if offline { self?.showToast("Offline - showing last saved data") }
            }
        }
    }
}

enum NavigationSelection: Hashable {
    case all, favorites
    case group(String)
    case channel(String)
}
