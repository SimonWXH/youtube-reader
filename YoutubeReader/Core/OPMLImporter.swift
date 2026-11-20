import Foundation

/// OPML is consumed locally only to discover channel IDs. No feed is ever fetched.
public enum OPMLImporter {
    public static func parse(data: Data) throws -> (groups: [ChannelGroup], channels: [Channel]) {
        let parser = OPMLParser()
        try parser.parse(data)
        var groups: [ChannelGroup] = []
        var channels: [Channel] = []
        for (groupName, entries) in parser.entries {
            let groupID = "group-" + stableID(groupName)
            var channelIDs: [String] = []
            for entry in entries {
                // Deliberately keep duplicates. The same channel can belong to multiple groups.
                if channels.contains(where: { $0.id == entry.id }) {
                    var existing = channels.first(where: { $0.id == entry.id })!
                    if !existing.groupIDs.contains(groupID) { existing.groupIDs.append(groupID) }
                    channels.removeAll { $0.id == entry.id }
                    channels.append(existing)
                } else {
                    channels.append(Channel(id: entry.id, title: entry.title, groupIDs: [groupID]))
                }
                channelIDs.append(entry.id)
            }
            groups.append(ChannelGroup(id: groupID, name: groupName, channelIDs: channelIDs))
        }
        return (groups, channels)
    }

    private static func stableID(_ name: String) -> String {
        let bytes = Array(name.utf8)
        var hash: UInt64 = 14695981039346656037
        for byte in bytes {
            hash ^= UInt64(byte)
            hash = hash &* 1099511628211
        }
        return String(format: "%016llx", hash)
    }

    private struct Entry { let id: String; let title: String }

    private final class OPMLParser: NSObject, XMLParserDelegate {
        var entries: [(String, [Entry])] = []
        private var groupStack: [String] = []
        private var currentGroup: String?
        private var currentAttributes: [String: String] = [:]
        private var depth = 0

        func parse(_ data: Data) throws {
            let parser = XMLParser(data: data)
            parser.delegate = self
            guard parser.parse(), parser.parserError == nil else { throw parser.parserError! }
        }

        func parser(_ parser: XMLParser, didStartElement elementName: String, namespaceURI: String?, qualifiedName qName: String?, attributes attributeDict: [String : String] = [:]) {
            depth += 1
            guard elementName == "outline" else { return }
            let title = attributeDict["title"] ?? attributeDict["text"] ?? ""
            currentAttributes = attributeDict
            if attributeDict["xmlUrl"] == nil {
                groupStack.append(title)
                currentGroup = title
                if !entries.contains(where: { $0.0 == title }) {
                    entries.append((title, []))
                }
            }
        }

        func parser(_ parser: XMLParser, didEndElement elementName: String, namespaceURI: String?, qualifiedName qName: String?) {
            guard elementName == "outline" else { depth -= 1; return }
            if currentAttributes["xmlUrl"] == nil {
                if !groupStack.isEmpty { groupStack.removeLast() }
                currentGroup = groupStack.last
            } else if let channelID = channelID(from: currentAttributes["xmlUrl"] ?? "") {
                let title = currentAttributes["title"] ?? currentAttributes["text"] ?? channelID
                let entry = Entry(id: channelID, title: title)
                if let group = currentGroup, let index = entries.firstIndex(where: { $0.0 == group }) {
                    entries[index].1.append(entry)
                }
            }
            currentAttributes = [:]
            depth -= 1
        }

        private func channelID(from xmlURL: String) -> String? {
            guard let url = URLComponents(string: xmlURL), let host = url.host,
                  host.contains("youtube.com") else { return nil }
            let id = url.queryItems?.first(where: { $0.name == "channel_id" })?.value
            return (id?.isEmpty == false) ? id : nil
        }
    }
}
