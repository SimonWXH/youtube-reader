import Foundation
import Network

/// Local-only connectivity monitor. Keeps the last successful data visible and emits
/// transitions used for the corner status icon and 2-second toast.
public final class ConnectivityMonitor {
    private let monitor = NWPathMonitor()
    private let queue = DispatchQueue(label: "com.simon.youtubereader.connectivity")
    private var wasOffline = false

    public init() {
        monitor.pathUpdateHandler = { [weak self] path in
            guard let self else { return }
            let offline = path.status != .satisfied
            if offline != self.wasOffline {
                self.wasOffline = offline
                NotificationCenter.default.post(name: .youtubeReaderConnectivityChanged, object: nil, userInfo: ["offline": offline])
            }
        }
        monitor.start(queue: queue)
    }

    deinit { monitor.cancel() }
}

public extension Notification.Name {
    static let youtubeReaderConnectivityChanged = Notification.Name("com.simon.youtubereader.connectivity")
}
