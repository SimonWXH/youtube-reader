import SwiftUI
import AppKit

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Settings").font(.title2.bold())
                Spacer()
                Button("Done") { dismiss() }
            }
            .padding()
            Divider()
            Form {
                Section("YouTube Data API") {
                    SecureField("API key (local only, never synced)", text: $model.settings.apiKey)
                    Text("Stored in Application Support. It is never written to the sync file.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                Section("Refresh") {
                    Picker("Interval", selection: $model.settings.refreshMinutes) {
                        Text("15 minutes").tag(15)
                        Text("30 minutes").tag(30)
                        Text("1 hour").tag(60)
                        Text("2 hours").tag(120)
                        Text("4 hours").tag(240)
                    }
                    Text("Videos older than 30 hours or beyond 400 items are pruned.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                Section("Files") {
                    PathField(title: "Sync JSON", text: $model.settings.syncFilePath)
                    PathField(title: "Favorites backup", text: $model.settings.favoritesBackupPath)
                    Button("Force take over sync") { model.forceTakeOverSync() }
                }
                Section("Content") {
                    Toggle("Show Shorts", isOn: $model.settings.showShorts)
                }
                Section("Notifications (local only)") {
                    ForEach(model.groups) { group in
                        VStack(alignment: .leading) {
                            HStack {
                                Text(group.name).font(.headline)
                                Spacer()
                                Button("Enable all") { model.enableNotifications(in: group) }
                            }
                            ForEach(group.channelIDs, id: \.self) { id in
                                let channel = model.channels.first { $0.id == id }
                                Toggle(channel?.title ?? id, isOn: Binding(
                                    get: { model.settings.notificationChannelIDs.contains(id) },
                                    set: { on in
                                        if on {
                                            model.settings.notificationChannelIDs.append(id)
                                        } else {
                                            model.settings.notificationChannelIDs.removeAll { $0 == id }
                                        }
                                    }
                                ))
                            }
                        }
                    }
                }
            }
            .formStyle(.grouped)
            HStack {
                Button("Save") { model.saveSettings(); dismiss() }
                    .buttonStyle(.borderedProminent)
                Spacer()
            }
            .padding()
        }
        .frame(width: 640, height: 700)
    }
}

struct PathField: View {
    let title: String
    @Binding var text: String

    var body: some View {
        HStack {
            TextField(title, text: $text)
            Button("...") {
                let panel = NSOpenPanel()
                panel.canChooseFiles = true
                panel.canChooseDirectories = false
                panel.begin { if $0 == .OK, let url = panel.url { text = url.path } }
            }
        }
    }
}
