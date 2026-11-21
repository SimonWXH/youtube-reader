// Draws the menu bar status item's four template images with SF Symbols, at
// 18 pt with the Regular weight, as 2x PNGs (36 x 36 pixels) of black ink on a
// transparent background. macOS tints a template image for the menu bar, so
// one file serves both appearances. Run it from the repository root:
//
//	swift Scripts/status-icons.swift
import AppKit

// state renders one symbol into a side-square PNG whose glyph keeps the
// proportions AppKit gives it at the point size.
func render(_ symbolName: String, pointSize: CGFloat, side: Int, to path: String) throws {
    let config = NSImage.SymbolConfiguration(pointSize: pointSize, weight: .regular)
    guard let symbol = NSImage(systemSymbolName: symbolName, accessibilityDescription: nil)?
        .withSymbolConfiguration(config) else {
        throw NSError(domain: symbolName, code: 1, userInfo: [NSLocalizedDescriptionKey: "no such symbol"])
    }
    guard let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil, pixelsWide: side, pixelsHigh: side,
        bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
        colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else {
        throw NSError(domain: symbolName, code: 2)
    }
    rep.size = NSSize(width: pointSize, height: pointSize)
    NSGraphicsContext.saveGraphicsState()
    defer { NSGraphicsContext.restoreGraphicsState() }
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    NSColor.clear.setFill()
    NSRect(x: 0, y: 0, width: pointSize, height: pointSize).fill()
    // Draw the symbol at its own proportions, centered in the point-size box,
    // then ink it black over its own alpha so the file is a template.
    let box = symbol.size
    let rect = NSRect(x: (pointSize - box.width) / 2, y: (pointSize - box.height) / 2,
                      width: box.width, height: box.height)
    symbol.draw(in: rect)
    NSColor.black.setFill()
    rect.fill(using: .sourceAtop)
    guard let data = rep.representation(using: .png, properties: [:]) else {
        throw NSError(domain: symbolName, code: 3)
    }
    try data.write(to: URL(fileURLWithPath: path))
}

let outDir = "assets/status"
try FileManager.default.createDirectory(atPath: outDir, withIntermediateDirectories: true)
// The four states' shapes, the SF Symbols the design board names.
let icons: [(symbol: String, file: String)] = [
    ("checkmark.circle", "up-to-date.png"),
    ("arrow.triangle.2.circlepath", "refreshing.png"),
    ("wifi.slash", "offline.png"),
    ("exclamationmark.triangle", "failed.png"),
]
for icon in icons {
    let path = "\(outDir)/\(icon.file)"
    try render(icon.symbol, pointSize: 18, side: 36, to: path)
    print("wrote \(path) (\(icon.symbol))")
}
