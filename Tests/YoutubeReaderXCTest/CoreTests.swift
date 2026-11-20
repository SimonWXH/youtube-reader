import Foundation
import XCTest
@testable import YoutubeReaderCore

final class CoreTests: XCTestCase {
    let iso: DateFormatter = {
        let f = DateFormatter(); f.dateFormat = "yyyy-MM-dd"; return f
    }()
    func date(_ s: String) -> Date { iso.date(from: s)! }

    func testRelativeDates() {
        let now = date("2025-01-10")
        XCTAssertEqual(DateFormatting.relative(now.addingTimeInterval(-30), now: now), "just now")
        XCTAssertEqual(DateFormatting.relative(now.addingTimeInterval(-3600 * 3), now: now), "3h ago")
        XCTAssertEqual(DateFormatting.relative(now.addingTimeInterval(-3600 * 50), now: now), "2d ago")
    }

    func testRetentionHoursAndCount() {
        let now = date("2025-01-10")
        let videos = (0..<500).map { i in
            Video(id: "v\(i)", channelID: "c", channelTitle: "c", title: "t", textDescription: "", thumbnailURL: "",
                  publishedAt: now.addingTimeInterval(-Double(i) * 60), duration: nil, isShort: false, createdAt: now)
        }
        let pruned = RetentionPolicy().prune(videos, now: now)
        XCTAssertEqual(pruned.count, 400)
        XCTAssertTrue(pruned.allSatisfy { $0.publishedAt >= now.addingTimeInterval(-30 * 3600) })
    }

    func testDedupeStableIDs() {
        let d = date("2025-01-01")
        let a = Video(id: "x", channelID: "c", channelTitle: "c", title: "old", textDescription: "", thumbnailURL: "", publishedAt: d, duration: nil, isShort: false, createdAt: d)
        let b = Video(id: "x", channelID: "c", channelTitle: "c", title: "new", textDescription: "", thumbnailURL: "", publishedAt: d.addingTimeInterval(60), duration: nil, isShort: false, createdAt: d)
        XCTAssertEqual(VideoDeduplication.dedupe([a, b]).count, 1)
        XCTAssertEqual(VideoDeduplication.dedupe([a, b])[0].title, "new")
    }

    func testQuota() {
        XCTAssertEqual(RefreshQuota.units(channelCount: 89, pagesPerChannel: 2, hydrationRequests: 3), 272)
        XCTAssertFalse(RefreshQuota.fits(272, remaining: 200))
    }

    func testSyncMergeReadWinsAndLastWriteWins() {
        let d = date("2025-01-01")
        var a = SyncSettings(deviceID: "A", updatedAt: d, groups: [], channels: [], readStates: [], favorites: [])
        var b = SyncSettings(deviceID: "B", updatedAt: d, groups: [], channels: [], readStates: [], favorites: [])
        a.groups = [ChannelGroupRecord(id: "g", name: "old", channelIDs: [], updatedAt: d)]
        b.groups = [ChannelGroupRecord(id: "g", name: "new", channelIDs: [], updatedAt: d.addingTimeInterval(10))]
        a.readStates = [ReadStateRecord(id: "A:v", videoID: "v", deviceID: "A", read: true, readAt: d)]
        b.readStates = [ReadStateRecord(id: "B:v", videoID: "v", deviceID: "B", read: false, readAt: d.addingTimeInterval(100))]
        let merged = SyncEngine.merge(local: a, remote: b)
        XCTAssertEqual(merged.groups[0].name, "new")
        XCTAssertEqual(merged.readStates.count, 1)
        XCTAssertTrue(merged.readStates[0].read)
    }

    func testSyncFileIdempotenceAndConflictCopies() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let file = dir.appendingPathComponent("sync.json")
        let store = SyncStore(fileURL: file, deviceID: "A")
        var state = store.empty()
        state.groups = [ChannelGroupRecord(id: "g", name: "Blender", channelIDs: [], updatedAt: Date())]
        try store.save(state)
        try store.save(state)
        XCTAssertEqual(try store.load().groups.count, 1)
        let conflict = dir.appendingPathComponent("sync.sync-conflict-20250101-000000.json")
        var other = state
        other.groups = [ChannelGroupRecord(id: "h", name: "GameDev", channelIDs: [], updatedAt: Date().addingTimeInterval(10))]
        let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
        try encoder.encode(other).write(to: conflict)
        XCTAssertEqual(Set(try store.load().groups.map(\.id)), ["g", "h"])
        try? FileManager.default.removeItem(at: dir)
    }

    func testOPMLPreservesGroupsAndDuplicates() throws {
        let url = URL(fileURLWithPath: "/Users/simon/Downloads/NetNewsWire.opml")
        guard FileManager.default.fileExists(atPath: url.path) else { return }
        let (groups, channels) = try OPMLImporter.parse(data: Data(contentsOf: url))
        XCTAssertEqual(groups.count, 7)
        XCTAssertEqual(channels.count, 87)
        let brackeys = channels.filter { $0.title == "Brackeys" }
        XCTAssertEqual(brackeys.count, 1)
        XCTAssertEqual(brackeys.first?.groupIDs.count, 2)
        let gameDev = groups.first { $0.name == "GameDev" }!
        XCTAssertEqual(gameDev.channelIDs.count, 17)
        XCTAssertEqual(Set(gameDev.channelIDs).count, 17)
    }

    func testShortDetection() {
        XCTAssertTrue(VideoFacts.isShort(duration: "PT45S", width: 1080, height: 1920))
        XCTAssertTrue(VideoFacts.isShort(duration: "PT3M", width: nil, height: nil))
        XCTAssertFalse(VideoFacts.isShort(duration: "PT8M12S", width: nil, height: nil))
    }
}
