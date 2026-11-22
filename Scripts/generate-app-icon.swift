// Deterministic app icon: 1024x1024 PNG.
// Rounded square with a diagonal #FF4E00 -> #D10B28 gradient and a centred
// white ring, based on the MVP icon's shape. No inner detail or text.
import AppKit

let side = 1024
let radius = CGFloat(side) * 0.2237       // Apple-style rounded corner
let tileInset = CGFloat(side) * 0.040     // canvas margin around the tile
let tileSide = CGFloat(side) - 2 * tileInset
let ringCenter = CGFloat(side) / 2
let ringOuter = CGFloat(side) * 0.305     // outside edge of the white ring
let ringInner = CGFloat(side) * 0.204     // transparent circular centre

guard let context = CGContext(data: nil, width: side, height: side,
                              bitsPerComponent: 8, bytesPerRow: 0,
                              space: CGColorSpaceCreateDeviceRGB(),
                              bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else {
    fatalError("could not create bitmap context")
}

// The gradient runs top-left to bottom-right. CGContext has its origin at the
// bottom-left, so its start is (0, side) and end is (side, 0).
let deviceRGB = CGColorSpaceCreateDeviceRGB()
func color(_ red: Int, _ green: Int, _ blue: Int) -> CGColor {
    CGColor(colorSpace: deviceRGB,
            components: [CGFloat(red)/255, CGFloat(green)/255, CGFloat(blue)/255, 1])!
}
let start = color(0xff, 0x4e, 0x00)
let end = color(0xd1, 0x0b, 0x28)
guard let gradient = CGGradient(colorsSpace: deviceRGB,
                                colors: [start, end] as CFArray,
                                locations: [0, 1]) else {
    fatalError("could not create gradient")
}

// Paint the gradient inside the rounded tile. The canvas corners stay clear.
let tileRect = CGRect(x: tileInset, y: tileInset, width: tileSide, height: tileSide)
let tilePath = CGPath(roundedRect: tileRect, cornerWidth: radius, cornerHeight: radius,
                      transform: nil)
context.addPath(tilePath)
context.clip()
context.drawLinearGradient(gradient, start: CGPoint(x: 0, y: side),
                           end: CGPoint(x: side, y: 0), options: [])

// The ring is the white area between the outer and inner circles. Filling
// both ellipses with the even-odd rule leaves the gradient visible through the
// centre instead of covering it with a white disc.
let outerRing = CGRect(x: ringCenter - ringOuter, y: ringCenter - ringOuter,
                       width: 2 * ringOuter, height: 2 * ringOuter)
let innerHole = CGRect(x: ringCenter - ringInner, y: ringCenter - ringInner,
                       width: 2 * ringInner, height: 2 * ringInner)
let ringPath = CGMutablePath()
ringPath.addEllipse(in: outerRing)
ringPath.addEllipse(in: innerHole)
context.addPath(ringPath)
context.setFillColor(NSColor.white.cgColor)
context.fillPath(using: .evenOdd)

guard let image = context.makeImage() else { fatalError("could not render icon") }
let rep = NSBitmapImageRep(cgImage: image)
guard let png = rep.representation(using: .png, properties: [:]) else {
    fatalError("could not encode PNG")
}
let out = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "resources/icon.png"
try png.write(to: URL(fileURLWithPath: out))
print("wrote \(out)")
