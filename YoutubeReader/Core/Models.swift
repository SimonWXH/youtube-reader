import Foundation

public struct Channel: Codable, Hashable, Identifiable, Sendable {
    public let id: String
    public var title: String
    public var groupIDs: [String]
    public var avatarURL: String?
    public var uploadedAt: Date?

    public init(id: String, title: String, groupIDs: [String], avatarURL: String? = nil, uploadedAt: Date? = nil) {
        self.id = id
        self.title = title
        self.groupIDs = groupIDs
        self.avatarURL = avatarURL
        self.uploadedAt = uploadedAt
    }
}

public struct ChannelGroup: Codable, Hashable, Identifiable, Sendable {
    public let id: String
    public var name: String
    public var channelIDs: [String]

    public init(id: String, name: String, channelIDs: [String]) {
        self.id = id
        self.name = name
        self.channelIDs = channelIDs
    }
}

public struct Video: Codable, Hashable, Identifiable, Sendable {
    public let id: String
    public var channelID: String
    public var channelTitle: String
    public var title: String
    public var textDescription: String
    public var thumbnailURL: String
    public var avatarURL: String?
    public var publishedAt: Date
    public var duration: TimeInterval?
    public var isShort: Bool
    public var createdAt: Date

    public init(id: String, channelID: String, channelTitle: String, title: String, textDescription: String,
                thumbnailURL: String, avatarURL: String? = nil, publishedAt: Date, duration: TimeInterval? = nil,
                isShort: Bool, createdAt: Date) {
        self.id = id
        self.channelID = channelID
        self.channelTitle = channelTitle
        self.title = title
        self.textDescription = textDescription
        self.thumbnailURL = thumbnailURL
        self.avatarURL = avatarURL
        self.publishedAt = publishedAt
        self.duration = duration
        self.isShort = isShort
        self.createdAt = createdAt
    }
}

public struct RefreshFailure: Codable, Hashable, Sendable {
    public var at: Date
    public var message: String

    public init(at: Date, message: String) {
        self.at = at
        self.message = message
    }
}
