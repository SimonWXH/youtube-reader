import SwiftUI
import YoutubeReaderCore

struct ContentView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        NavigationSplitView {
            Sidebar()
                .frame(minWidth: 210)
        } detail: {
            VideoGridView()
                .frame(minWidth: 600)
        }
        .navigationTitle(model.title)
        .sheet(isPresented: $model.showSettings) { SettingsView().environmentObject(model) }
        .overlay(alignment: .bottom) {
            if let toast = model.toast {
                Text(toast)
                    .font(.callout)
                    .padding(.horizontal, 16).padding(.vertical, 9)
                    .background(.regularMaterial, in: Capsule())
                    .overlay(Capsule().stroke(.quaternary))
                    .padding(.bottom, 22)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .animation(.easeOut(duration: 0.2), value: model.toast)
    }
}

struct Sidebar: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        List(selection: Binding(get: { model.selection }, set: { model.selection = $0 })) {
            Section {
                Label("All", systemImage: "square.grid.2x2").tag(NavigationSelection.all)
                Label("Favorites", systemImage: "star").tag(NavigationSelection.favorites)
            }
            ForEach(model.groups) { group in
                Section(group.name) {
                    ForEach(group.channelIDs, id: \.self) { id in
                        let channel = model.channels.first { $0.id == id }
                        Label(channel?.title ?? id, systemImage: "play.rectangle")
                            .lineLimit(1)
                            .tag(NavigationSelection.channel(id))
                    }
                }
                .tag(NavigationSelection.group(group.id))
            }
            Section {
                StatusView()
            }
        }
        .listStyle(.sidebar)
        .toolbar {
            ToolbarItem(placement: .navigation) {
                Button(action: model.importOPML) { Label("Import OPML", systemImage: "square.and.arrow.down") }
                    .help("Import YouTube channels from an OPML file")
            }
        }
    }
}

struct StatusView: View {
    @EnvironmentObject var model: AppModel
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation { expanded.toggle() }
            } label: {
                HStack {
                    Label("Status", systemImage: model.isOffline ? "wifi.slash" : (model.isRefreshing ? "arrow.triangle.2.circlepath" : "checkmark.circle"))
                    Spacer()
                    Image(systemName: expanded ? "chevron.up" : "chevron.down")
                }
                .font(.callout)
            }
            .buttonStyle(.plain)
            if expanded {
                VStack(alignment: .leading, spacing: 3) {
                    statusLine("Last refresh", model.settings.lastRefresh.map(DateFormatting.time) ?? "never")
                    statusLine("Next refresh", model.settings.nextRefresh.map(DateFormatting.time) ?? "not scheduled")
                    if model.settings.recentFailures.isEmpty {
                        Text("No recent failures").font(.caption2).foregroundStyle(.secondary)
                    } else {
                        Text("Recent failures:").font(.caption2).foregroundStyle(.secondary)
                        ForEach(Array(model.settings.recentFailures.prefix(3).enumerated()), id: \.offset) { _, failure in
                            Text("\(DateFormatting.time(failure.at)) \(failure.message)")
                                .font(.caption2).foregroundStyle(.red).lineLimit(2)
                        }
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func statusLine(_ label: String, _ value: String) -> some View {
        HStack { Text(label).font(.caption2).foregroundStyle(.secondary); Spacer(); Text(value).font(.caption2) }
    }
}
