import Foundation

/// Shared refresh implementation used by the app and the LaunchAgent helper.
/// The helper holds the SQLite write lock; the app holds the sync-file flock while open.
public final class RefreshService {
    public let api: YouTubeAPI
    public let database: Database
    public let syncStore: SyncStore
    public let settings: LocalSettings
    public let onFailure: ((String) -> Void)?

    public init(api: YouTubeAPI, database: Database, syncStore: SyncStore, settings: LocalSettings, onFailure: ((String) -> Void)? = nil) {
        self.api = api
        self.database = database
        self.syncStore = syncStore
        self.settings = settings
        self.onFailure = onFailure
    }

    public struct RefreshResult {
        public var newVideos: [String: Int] = [:]
        public var failures: [String] = []
        public var hydratedFavoriteIDs: Set<String> = []
    }

    public func refresh(channelIDs: [String], force: Bool = false) async -> RefreshResult {
        var result = RefreshResult()
        do {
            try database.open()
            var state = try syncStore.load()
            let now = Date()
            let last = database.value(key: "lastRefresh").flatMap(Double.init).map(Date.init(timeIntervalSince1970:)) ?? .distantPast
            let interval = Double(settings.refreshMinutes) * 60
            if !force, now.timeIntervalSince(last) < interval {
                return result
            }
            var stored = Dictionary(uniqueKeysWithValues: (try database.channels()).map { ($0.id, $0) })
            let allChannels = try await api.channels(ids: channelIDs)
            for remote in allChannels {
                let snippet = remote.snippet
                if var channel = stored[remote.id] {
                    channel.title = snippet?.title ?? channel.title
                    channel.avatarURL = snippet?.thumbnails?.medium?.url ?? snippet?.thumbnails?.default?.url
                    stored[remote.id] = channel
                }
            }
            try database.insertChannels(stored.values.map { $0 as Channel })
            for channel in stored.values {
                if let avatar = channel.avatarURL { try? database.setChannelAvatar(channel.id, url: avatar) }
            }

            var fetched: [Video] = []
            for channelID in channelIDs {
                do {
                    guard let playlist = try await api.uploadsPlaylistID(channelID: channelID) else {
                        result.failures.append("No uploads playlist for \(stored[channelID]?.title ?? channelID)")
                        continue
                    }
                    var token: String? = nil
                    var oldest = Date.distantFuture
                    var pages = 0
                    repeat {
                        let page = try await api.playlistItems(playlistID: playlist, pageToken: token)
                        pages += 1
                        for item in page.items ?? [] {
                            guard let snippet = item.snippet,
                                  let videoID = item.contentDetails?.videoId ?? snippet.resourceId?.videoId,
                                  let published = snippet.publishedAt else { continue }
                            oldest = min(oldest, published)
                            let thumb = snippet.thumbnails?.medium?.url ?? snippet.thumbnails?.high?.url ?? snippet.thumbnails?.default?.url ?? ""
                            fetched.append(Video(
                                id: videoID,
                                channelID: channelID,
                                channelTitle: snippet.channelTitle ?? stored[channelID]?.title ?? channelID,
                                title: snippet.title ?? videoID,
                                textDescription: snippet.description ?? "",
                                thumbnailURL: thumb,
                                avatarURL: stored[channelID]?.avatarURL,
                                publishedAt: published,
                                duration: nil,
                                isShort: VideoFacts.isShort(duration: nil, width: nil, height: nil),
                                createdAt: now
                            ))
                        }
                        token = page.nextPageToken
                    } while token != nil && pages < 3 && oldest > now.addingTimeInterval(-30 * 24 * 3600)
                    try database.setChannelUploadedAt(channelID, date: now, playlist: playlist)
                } catch {
                    let message = "\(stored[channelID]?.title ?? channelID): \(error.localizedDescription)"
                    result.failures.append(message)
                    onFailure?(message)
                }
            }

            let existingIDs = Set((try database.videos()).map(\.id))
            let unique = VideoDeduplication.dedupe(fetched)
            try database.upsertVideos(unique)
            let newByChannel = Dictionary(grouping: unique.filter { !existingIDs.contains($0.id) }) { $0.channelID }
            result.newVideos = newByChannel.mapValues(\.count)

            // Hydrate favorites whose original metadata has expired or was never stored.
            let missingFavorites = state.favorites.compactMap { record -> String? in record.video == nil ? record.id : nil }
            if !missingFavorites.isEmpty {
                let resources = (try? await api.videos(ids: missingFavorites)) ?? []
                var updated = state
                for resource in resources {
                    guard let snippet = resource.snippet, let published = snippet.publishedAt else { continue }
                    let duration = VideoFacts.durationSeconds(resource.contentDetails?.duration)
                    let thumb = snippet.thumbnails?.medium?.url ?? snippet.thumbnails?.default?.url ?? ""
                    let video = Video(
                        id: resource.id, channelID: snippet.channelId ?? "", channelTitle: snippet.channelTitle ?? "",
                        title: snippet.title ?? resource.id, textDescription: snippet.description ?? "",
                        thumbnailURL: thumb, avatarURL: nil, publishedAt: published, duration: duration,
                        isShort: VideoFacts.isShort(duration: resource.contentDetails?.duration, width: nil, height: nil),
                        createdAt: now)
                    if let index = updated.favorites.firstIndex(where: { $0.id == resource.id }) {
                        updated.favorites[index].video = video
                        updated.favorites[index].updatedAt = now
                    }
                    result.hydratedFavoriteIDs.insert(resource.id)
                }
                state = updated
            }

            let allVideos = try database.videos()
            let retained = RetentionPolicy().prune(allVideos, now: now)
            let removed = Set(allVideos.map(\.id)).subtracting(retained.map(\.id))
            if !removed.isEmpty { try database.deleteVideos(ids: Array(removed)) }

            try database.setValue(String(now.timeIntervalSince1970), key: "lastRefresh")
            var local = settings
            local.lastRefresh = now
            local.nextRefresh = now.addingTimeInterval(Double(local.refreshMinutes) * 60)
            local.recentFailures = Array((result.failures.map { RefreshFailure(at: now, message: $0) } + local.recentFailures).prefix(10))
            try local.save()
            try syncStore.save(state)

            for (channelID, count) in result.newVideos where settings.notificationChannelIDs.contains(channelID) {
                let title = stored[channelID]?.title ?? channelID
                Notifier.postNewVideos(channelTitle: title, count: count, channelID: channelID)
            }
            if !result.failures.isEmpty { Notifier.postRefreshFailure(result.failures.joined(separator: "\n")) }
        } catch {
            let message = error.localizedDescription
            result.failures.append(message)
            onFailure?(message)
            Notifier.postRefreshFailure(message)
        }
        return result
    }
}

public extension VideoFacts {
    static func durationSeconds(_ iso: String?) -> TimeInterval? {
        guard let iso else { return nil }
        let pattern = try? NSRegularExpression(pattern: "PT(?:(\\d+)H)?(?:(\\d+)M)?(?:(\\d+(?:\\.\\d+)?)S)?")
        guard let match = pattern?.firstMatch(in: iso, range: NSRange(iso.startIndex..., in: iso)) else { return nil }
        func value(_ i: Int) -> Double {
            guard let r = Range(match.range(at: i), in: iso) else { return 0 }
            return Double(iso[r]) ?? 0
        }
        return value(1) * 3600 + value(2) * 60 + value(3)
    }
}
