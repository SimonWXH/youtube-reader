import Foundation
import UserNotifications

public enum Notifier {
    public static func requestAuthorization() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in }
    }

    public static func postNewVideos(channelTitle: String, count: Int, channelID: String) {
        let content = UNMutableNotificationContent()
        content.title = channelTitle
        content.body = count == 1 ? "1 new video" : "\(count) new videos"
        content.sound = .default
        content.userInfo = ["channelID": channelID]
        let request = UNNotificationRequest(identifier: "new-\(channelID)-\(Date().timeIntervalSince1970)", content: content, trigger: nil)
        UNUserNotificationCenter.current().add(request)
    }

    public static func postRefreshFailure(_ message: String) {
        let content = UNMutableNotificationContent()
        content.title = "YouTube Reader refresh failed"
        content.body = message
        content.sound = .default
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "failure-\(UUID().uuidString)", content: content, trigger: nil))
    }
}
