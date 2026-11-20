import Foundation

public enum APIError: LocalizedError {
    case missingKey
    case invalidResponse
    case httpStatus(Int, String)
    case quotaExceeded
    case offline

    public var errorDescription: String? {
        switch self {
        case .missingKey: return "Add a YouTube Data API key in Settings."
        case .invalidResponse: return "The API returned an unexpected response."
        case .httpStatus(let code, let detail): return "API error \(code): \(detail)"
        case .quotaExceeded: return "The YouTube API daily quota was exceeded. Refresh will resume tomorrow."
        case .offline: return "You are offline. The last successful data is still available."
        }
    }
}

/// Firebase-style YouTube Data API v3 client. No RSS, no local HTTP server.
public struct YouTubeAPI: Sendable {
    public static let base = "https://www.googleapis.com/youtube/v3"
    public let key: String
    public let session: URLSession
    public let decoder: JSONDecoder

    public init(key: String, session: URLSession = .shared) {
        self.key = key
        self.session = session
        let decoder = JSONDecoder()
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        decoder.dateDecodingStrategy = .custom { decoder in
            let text = try decoder.singleValueContainer().decode(String.self)
            if let d = formatter.date(from: text) { return d }
            let fallback = ISO8601DateFormatter()
            fallback.formatOptions = [.withInternetDateTime]
            if let d = fallback.date(from: text) { return d }
            throw DecodingError.dataCorruptedError(in: try decoder.singleValueContainer(), debugDescription: "Bad date: \(text)")
        }
        self.decoder = decoder
    }

    public func uploadsPlaylistID(channelID: String) async throws -> String? {
        let data = try await get("channels", [
            "part": "contentDetails",
            "id": channelID
        ])
        let response = try decoder.decode(ChannelsResponse.self, from: data)
        return response.items?.first?.contentDetails?.relatedPlaylists?.uploads
    }

    public func playlistItems(playlistID: String, pageToken: String?) async throws -> PlaylistItemsResponse {
        var query: [String: String] = [
            "part": "snippet,contentDetails",
            "playlistId": playlistID,
            "maxResults": "50"
        ]
        if let pageToken { query["pageToken"] = pageToken }
        let data = try await get("playlistItems", query)
        return try decoder.decode(PlaylistItemsResponse.self, from: data)
    }

    public func videos(ids: [String]) async throws -> [VideoResource] {
        guard !ids.isEmpty else { return [] }
        var all: [VideoResource] = []
        for chunk in ids.chunked(into: 50) {
            let data = try await get("videos", [
                "part": "snippet,contentDetails",
                "id": chunk.joined(separator: ","),
                "maxResults": "50"
            ])
            let response = try decoder.decode(VideosResponse.self, from: data)
            all.append(contentsOf: response.items ?? [])
        }
        return all
    }

    public func channels(ids: [String]) async throws -> [ChannelResource] {
        guard !ids.isEmpty else { return [] }
        var all: [ChannelResource] = []
        for chunk in ids.chunked(into: 50) {
            let data = try await get("channels", [
                "part": "snippet,contentDetails",
                "id": chunk.joined(separator: ","),
                "maxResults": "50"
            ])
            let response = try decoder.decode(ChannelsResponse.self, from: data)
            all.append(contentsOf: response.items ?? [])
        }
        return all
    }

    private func get(_ path: String, _ query: [String: String]) async throws -> Data {
        guard !key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { throw APIError.missingKey }
        var components = URLComponents(string: "\(Self.base)/\(path)")!
        components.queryItems = query.map { URLQueryItem(name: $0.key, value: $0.value) } + [URLQueryItem(name: "key", value: key)]
        var request = URLRequest(url: components.url!)
        request.timeoutInterval = 20
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw APIError.invalidResponse }
        if http.statusCode == 403 || http.statusCode == 429 {
            let detail = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["error"] as? [String: Any]
            let message = (detail?["message"] as? String) ?? "Forbidden"
            if message.localizedCaseInsensitiveContains("quota") { throw APIError.quotaExceeded }
            throw APIError.httpStatus(http.statusCode, message)
        }
        guard (200..<300).contains(http.statusCode) else {
            let detail = String(data: data, encoding: .utf8)?.prefix(200) ?? ""
            throw APIError.httpStatus(http.statusCode, String(detail))
        }
        return data
    }
}

public extension Array {
    func chunked(into size: Int) -> [[Element]] {
        stride(from: 0, to: count, by: size).map { Array(self[$0..<Swift.min($0 + size, count)]) }
    }
}

public struct ChannelsResponse: Decodable { let items: [ChannelResource]? }
public struct VideosResponse: Decodable { let items: [VideoResource]? }

public struct ChannelResource: Decodable {
    public let id: String
    public let snippet: ChannelSnippet?
    public let contentDetails: ChannelContentDetails?
}
public struct ChannelSnippet: Decodable { let title: String?; let thumbnails: Thumbnails? }
public struct ChannelContentDetails: Decodable { let relatedPlaylists: RelatedPlaylists? }
public struct RelatedPlaylists: Decodable { let uploads: String? }

public struct PlaylistItemsResponse: Decodable {
    public let items: [PlaylistItem]?
    public let nextPageToken: String?
}
public struct PlaylistItem: Decodable {
    public let snippet: PlaylistSnippet?
    public let contentDetails: PlaylistContentDetails?
}
public struct PlaylistSnippet: Decodable {
    public let title: String?
    public let description: String?
    public let publishedAt: Date?
    public let thumbnails: Thumbnails?
    public let channelId: String?
    public let channelTitle: String?
    public let resourceId: ResourceID?
}
public struct PlaylistContentDetails: Decodable { let videoId: String? }
public struct ResourceID: Decodable { let videoId: String? }

public struct VideoResource: Decodable {
    public let id: String
    public let snippet: VideoSnippet?
    public let contentDetails: VideoContentDetails?
}
public struct VideoSnippet: Decodable {
    public let title: String?
    public let description: String?
    public let publishedAt: Date?
    public let thumbnails: Thumbnails?
    public let channelId: String?
    public let channelTitle: String?
}
public struct VideoContentDetails: Decodable { let duration: String? }

public struct Thumbnails: Decodable {
    public let medium: Thumbnail?
    public let high: Thumbnail?
    public let `default`: Thumbnail?
}
public struct Thumbnail: Decodable { let url: String? }

public enum VideoFacts {
    /// ISO-8601 durations and portrait thumbnails identify Shorts. Kept pure for testing.
    public static func isShort(duration: String?, width: Int?, height: Int?) -> Bool {
        if let w = width, let h = height, h > w { return true }
        guard let duration else { return false }
        let pattern = try? NSRegularExpression(pattern: "PT(?:(\\d+)H)?(?:(\\d+)M)?(?:(\\d+(?:\\.\\d+)?)S)?")
        guard let match = pattern?.firstMatch(in: duration, range: NSRange(duration.startIndex..., in: duration)),
              match.numberOfRanges > 3 else { return false }
        func value(_ i: Int) -> Double {
            guard let r = Range(match.range(at: i), in: duration) else { return 0 }
            return Double(duration[r]) ?? 0
        }
        let seconds = value(1) * 3600 + value(2) * 60 + value(3)
        return seconds > 0 && seconds <= 180
    }
}
