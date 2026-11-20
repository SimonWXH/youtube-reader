import Foundation
import YoutubeReaderCore

private func assertEqual<T: Equatable>(_ a: T, _ b: T, _ message: String = "") {
    if a != b { fatalError("assertEqual failed: \(a) != \(b) \(message)") }
}
private func assertTrue(_ a: Bool, _ message: String = "") {
    if !a { fatalError("assertTrue failed \(message)") }
}
private func assertFalse(_ a: Bool, _ message: String = "") {
    if a { fatalError("assertFalse failed \(message)") }
}

let iso: DateFormatter = { let f = DateFormatter(); f.dateFormat = "yyyy-MM-dd"; return f }()
func date(_ s: String) -> Date { iso.date(from: s)! }
func testRelativeDates() throws {
            let now = date("2025-01-10")
            assertEqual(DateFormatting.relative(now.addingTimeInterval(-30), now: now), "just now")
            assertEqual(DateFormatting.relative(now.addingTimeInterval(-3600 * 3), now: now), "3h ago")
            assertEqual(DateFormatting.relative(now.addingTimeInterval(-3600 * 50), now: now), "2d ago")
        }

func testRetentionHoursAndCount() throws {
            let now = date("2025-01-10")
            let videos = (0..<500).map { i in
                Video(id: "v\(i)", channelID: "c", channelTitle: "c", title: "t", textDescription: "", thumbnailURL: "",
                      publishedAt: now.addingTimeInterval(-Double(i) * 60), duration: nil, isShort: false, createdAt: now)
            }
            let pruned = RetentionPolicy().prune(videos, now: now)
            assertEqual(pruned.count, 400)
            assertTrue(pruned.allSatisfy { $0.publishedAt >= now.addingTimeInterval(-30 * 3600) })
        }

func testDedupeStableIDs() throws {
            let d = date("2025-01-01")
            let a = Video(id: "x", channelID: "c", channelTitle: "c", title: "old", textDescription: "", thumbnailURL: "", publishedAt: d, duration: nil, isShort: false, createdAt: d)
            let b = Video(id: "x", channelID: "c", channelTitle: "c", title: "new", textDescription: "", thumbnailURL: "", publishedAt: d.addingTimeInterval(60), duration: nil, isShort: false, createdAt: d)
            assertEqual(VideoDeduplication.dedupe([a, b]).count, 1)
            assertEqual(VideoDeduplication.dedupe([a, b])[0].title, "new")
        }

func testQuota() throws {
            assertEqual(RefreshQuota.units(channelCount: 89, pagesPerChannel: 2, hydrationRequests: 3), 272)
            assertFalse(RefreshQuota.fits(272, remaining: 200))
        }

func testSyncMergeReadWinsAndLastWriteWins() throws {
            let d = date("2025-01-01")
            var a = SyncSettings(deviceID: "A", updatedAt: d, groups: [], channels: [], readStates: [], favorites: [])
            var b = SyncSettings(deviceID: "B", updatedAt: d, groups: [], channels: [], readStates: [], favorites: [])
            a.groups = [ChannelGroupRecord(id: "g", name: "old", channelIDs: [], updatedAt: d)]
            b.groups = [ChannelGroupRecord(id: "g", name: "new", channelIDs: [], updatedAt: d.addingTimeInterval(10))]
            a.readStates = [ReadStateRecord(id: "A:v", videoID: "v", deviceID: "A", read: true, readAt: d)]
            b.readStates = [ReadStateRecord(id: "B:v", videoID: "v", deviceID: "B", read: false, readAt: d.addingTimeInterval(100))]
            let merged = SyncEngine.merge(local: a, remote: b)
            assertEqual(merged.groups[0].name, "new")
            assertEqual(merged.readStates.count, 1)
            assertTrue(merged.readStates[0].read)
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
            assertEqual(try store.load().groups.count, 1)
            let conflict = dir.appendingPathComponent("sync.sync-conflict-20250101-000000.json")
            var other = state
            other.groups = [ChannelGroupRecord(id: "h", name: "GameDev", channelIDs: [], updatedAt: Date().addingTimeInterval(10))]
            let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
            try encoder.encode(other).write(to: conflict)
            assertEqual(Set(try store.load().groups.map(\.id)), ["g", "h"])
            try? FileManager.default.removeItem(at: dir)
        }

func testOPMLPreservesGroupsAndDuplicates() throws {
            let url = URL(fileURLWithPath: "/Users/simon/Downloads/NetNewsWire.opml")
            guard FileManager.default.fileExists(atPath: url.path) else { return }
            let (groups, channels) = try OPMLImporter.parse(data: Data(contentsOf: url))
            assertEqual(groups.count, 7)
            assertEqual(channels.count, 87)
            let brackeys = channels.filter { $0.title == "Brackeys" }
            assertEqual(brackeys.count, 1)
            assertEqual(brackeys.first?.groupIDs.count, 2)
            let gameDev = groups.first { $0.name == "GameDev" }!
            assertEqual(gameDev.channelIDs.count, 17)
            // Duplicate IDs remain in group membership arrays.
            assertEqual(Set(gameDev.channelIDs).count, 17, "GameDev members should be unique")
        }

func testShortDetection() throws {
            assertTrue(VideoFacts.isShort(duration: "PT45S", width: 1080, height: 1920))
            assertTrue(VideoFacts.isShort(duration: "PT3M", width: nil, height: nil))
            assertFalse(VideoFacts.isShort(duration: "PT8M12S", width: nil, height: nil))
        }


let checks: [(String, () throws -> Void)] = [
    ("testRelativeDates", testRelativeDates),
    ("testRetentionHoursAndCount", testRetentionHoursAndCount),
    ("testDedupeStableIDs", testDedupeStableIDs),
    ("testQuota", testQuota),
    ("testSyncMergeReadWinsAndLastWriteWins", testSyncMergeReadWinsAndLastWriteWins),
    ("testSyncFileIdempotenceAndConflictCopies", testSyncFileIdempotenceAndConflictCopies),
    ("testOPMLPreservesGroupsAndDuplicates", testOPMLPreservesGroupsAndDuplicates),
    ("testShortDetection", testShortDetection),
]
var failed = 0
for (name, check) in checks {
    do { try check(); print("PASS \(name)") } catch { print("FAIL \(name): \(error)"); failed += 1 }
}
if failed > 0 { exit(1) }
