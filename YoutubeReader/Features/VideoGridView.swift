import SwiftUI
import YoutubeReaderCore

struct VideoGridView: View {
    @EnvironmentObject var model: AppModel

    private let columns = [GridItem(.adaptive(minimum: 260, maximum: 360), spacing: 18)]

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                TextField("Search videos", text: $model.search)
                    .textFieldStyle(.roundedBorder)
                    .frame(maxWidth: 320)
                Toggle("Unread only", isOn: $model.unreadOnly)
                    .toggleStyle(.checkbox)
                Toggle("Show Shorts", isOn: $model.settings.showShorts)
                    .toggleStyle(.checkbox)
                    .onChange(of: model.settings.showShorts) { _ in try? model.settings.save() }
                Picker("Channel", selection: Binding(get: {
                    if case .channel(let id) = model.selection { return id }
                    return "all"
                }, set: { value in
                    model.selection = value == "all" ? .all : .channel(value)
                })) {
                    Text("All channels").tag("all")
                    ForEach(model.channels) { channel in
                        Text(channel.title).tag(channel.id)
                    }
                }
                .frame(maxWidth: 180)
                Spacer()
                Button { Task { await model.refresh() } } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                .disabled(model.isRefreshing)
                Button { model.showSettings = true } label: {
                    Label("Settings", systemImage: "gear")
                }
            }
            .padding()
            Divider()
            ScrollView {
                LazyVGrid(columns: columns, spacing: 20) {
                    ForEach(model.visibleVideos) { video in
                        VideoCard(video: video)
                            .onTapGesture { model.selectedVideo = video }
                    }
                }
                .padding(20)
            }
            .overlay {
                if model.visibleVideos.isEmpty {
                    VStack(spacing: 12) {
                        Image(systemName: "play.rectangle.on.rectangle").font(.system(size: 42)).foregroundStyle(.secondary)
                        Text(model.search.isEmpty ? "No videos yet" : "No matching videos").font(.title3)
                        Text("Import an OPML file and add an API key in Settings to begin.")
                            .font(.callout).foregroundStyle(.secondary)
                    }
                }
            }
        }
        .safeAreaInset(edge: .trailing, spacing: 0) {
            if let video = model.selectedVideo {
                VideoDetail(video: video)
                    .frame(width: 380)
                    .background(.background)
                    .overlay(Divider(), alignment: .leading)
                    .transition(.move(edge: .trailing))
            }
        }
    }
}

struct VideoCard: View {
    @EnvironmentObject var model: AppModel
    let video: Video

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            AsyncImage(url: URL(string: video.thumbnailURL)) { image in
                image.resizable().scaledToFill()
            } placeholder: {
                Rectangle().fill(.quaternary).overlay(ProgressView())
            }
            .frame(height: 150)
            .clipShape(RoundedRectangle(cornerRadius: 10))
            HStack(alignment: .top, spacing: 8) {
                AsyncImage(url: URL(string: video.avatarURL ?? "")) { image in
                    image.resizable().scaledToFill()
                } placeholder: {
                    Circle().fill(.quaternary)
                }
                .frame(width: 26, height: 26)
                .clipShape(Circle())
                VStack(alignment: .leading, spacing: 3) {
                    Text(video.title).font(.headline).lineLimit(2)
                    HStack {
                        Text(video.channelTitle).font(.caption).foregroundStyle(.secondary)
                        Text("·").font(.caption).foregroundStyle(.secondary)
                        Text(DateFormatting.relative(video.publishedAt)).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if !model.isRead(video) { Circle().fill(Color.accentColor).frame(width: 8, height: 8) }
            }
            Text(video.textDescription.isEmpty ? "No description" : video.textDescription)
                .font(.caption).foregroundStyle(.secondary).lineLimit(2)
        }
        .padding(10)
        .background(.background, in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).stroke(.quaternary))
        .onAppear { if !model.isRead(video) { model.markRead(video) } }
    }
}

struct VideoDetail: View {
    @EnvironmentObject var model: AppModel
    let video: Video

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                AsyncImage(url: URL(string: video.thumbnailURL)) { image in
                    image.resizable().scaledToFit()
                } placeholder: { Rectangle().fill(.quaternary) }
                .clipShape(RoundedRectangle(cornerRadius: 10))
                Text(video.title).font(.title2.bold())
                HStack {
                    Text(video.channelTitle).foregroundStyle(.secondary)
                    Text(DateFormatting.relative(video.publishedAt)).foregroundStyle(.secondary)
                }
                Divider()
                Text(video.textDescription.isEmpty ? "No description provided." : video.textDescription)
                    .font(.body)
                Spacer(minLength: 20)
                HStack {
                    Button {
                        let url = URL(string: "https://www.youtube.com/watch?v=\(video.id)")!
                        NSWorkspace.shared.open(url)
                    } label: {
                        Label("Open in YouTube", systemImage: "safari")
                    }
                    .buttonStyle(.borderedProminent)
                    Button {
                        model.toggleFavorite(video)
                    } label: {
                        Label(model.isFavorite(video) ? "Unfavorite" : "Favorite",
                              systemImage: model.isFavorite(video) ? "star.fill" : "star")
                    }
                }
                Spacer()
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
