import Foundation

public struct RetentionPolicy {
    public var hours: Double = 30
    public var maxVideos: Int = 400

    public func prune(_ videos: [Video], now: Date = Date()) -> [Video] {
        let cutoff = now.addingTimeInterval(-hours * 3600)
        let fresh = videos.filter { $0.publishedAt >= cutoff }
        return Array(fresh.sorted { $0.publishedAt > $1.publishedAt }.prefix(maxVideos))
    }

    public init(hours: Double = 30, maxVideos: Int = 400) {
        self.hours = hours
        self.maxVideos = maxVideos
    }
}


public enum VideoDeduplication {
    /// Stable IDs across API and hydration paths; newest publishedAt wins for the same ID.
    public static func dedupe(_ videos: [Video]) -> [Video] {
        var byID: [String: Video] = [:]
        for video in videos {
            if let existing = byID[video.id], existing.publishedAt >= video.publishedAt { continue }
            byID[video.id] = video
        }
        return byID.values.sorted { $0.publishedAt > $1.publishedAt }
    }
}

public enum RefreshQuota {
    /// Approximate Data API units. Metadata calls are batched 50 IDs per request and each request
    /// costs 1 unit; uploads playlist lookups and playlist pages cost 1 unit each. Hydration is
    /// passed as a request count, not a video count.
    public static func units(channelCount: Int, pagesPerChannel: Int, hydrationRequests: Int) -> Int {
        let channels = max(0, channelCount)
        let metadataBatches = (channels + 49) / 50
        return metadataBatches + channels * (1 + max(0, pagesPerChannel)) + max(0, hydrationRequests)
    }

    public static func fits(_ needed: Int, remaining: Int) -> Bool { needed <= max(0, remaining) }
}

public enum DateFormatting {
    public static func relative(_ date: Date, now: Date = Date()) -> String {
        let seconds = max(0, Int(now.timeIntervalSince(date)))
        if seconds < 60 { return "just now" }
        let minutes = seconds / 60
        if minutes < 60 { return "\(minutes)m ago" }
        let hours = minutes / 60
        if hours < 24 { return "\(hours)h ago" }
        let days = hours / 24
        if days < 7 { return "\(days)d ago" }
        let weeks = days / 7
        if weeks < 5 { return "\(weeks)w ago" }
        let months = days / 30
        if months < 12 { return "\(months)mo ago" }
        return "\(days / 365)y ago"
    }

    public static func time(_ date: Date) -> String {
        let f = DateFormatter()
        f.timeStyle = .short
        return f.string(from: date)
    }
}
