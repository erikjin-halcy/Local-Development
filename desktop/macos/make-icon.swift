// Ritar appikonen (mörk ruta med tre "panes" och en branch-linje) och skriver en .iconset-katalog.
// Användning: swift make-icon.swift <ut.iconset>
import AppKit

let out = CommandLine.arguments[1]
try? FileManager.default.createDirectory(atPath: out, withIntermediateDirectories: true)

func draw(_ size: CGFloat) -> Data {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size), pixelsHigh: Int(size), bitsPerSample: 8,
                               samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    let s = size / 1024
    let inset = 100 * s
    let rect = NSRect(x: inset, y: inset, width: size - 2 * inset, height: size - 2 * inset)
    let bg = NSBezierPath(roundedRect: rect, xRadius: 185 * s, yRadius: 185 * s)
    NSGradient(starting: NSColor(red: 0.10, green: 0.14, blue: 0.19, alpha: 1),
               ending: NSColor(red: 0.04, green: 0.06, blue: 0.08, alpha: 1))!.draw(in: bg, angle: -90)
    NSColor(white: 1, alpha: 0.08).setStroke(); bg.lineWidth = 6 * s; bg.stroke()

    // Tre panes som i en tmux-layout
    let pad = 70 * s, gap = 26 * s
    let inner = rect.insetBy(dx: pad, dy: pad)
    let leftW = (inner.width - gap) * 0.55
    let panes: [(NSRect, NSColor)] = [
        (NSRect(x: inner.minX, y: inner.minY, width: leftW, height: inner.height), NSColor(red: 0.37, green: 0.70, blue: 1, alpha: 1)),
        (NSRect(x: inner.minX + leftW + gap, y: inner.midY + gap / 2, width: inner.width - leftW - gap, height: inner.height / 2 - gap / 2), NSColor(red: 0.49, green: 0.91, blue: 0.53, alpha: 1)),
        (NSRect(x: inner.minX + leftW + gap, y: inner.minY, width: inner.width - leftW - gap, height: inner.height / 2 - gap / 2), NSColor(red: 0.78, green: 0.57, blue: 0.92, alpha: 1)),
    ]
    for (r, c) in panes {
        let p = NSBezierPath(roundedRect: r, xRadius: 40 * s, yRadius: 40 * s)
        c.withAlphaComponent(0.16).setFill(); p.fill()
        c.setStroke(); p.lineWidth = 10 * s; p.stroke()
    }
    // Branch-symbol i den stora panen
    let a = panes[0].0
    let x = a.minX + a.width * 0.38, top = a.maxY - a.height * 0.22, bottom = a.minY + a.height * 0.22
    let line = NSBezierPath()
    line.move(to: NSPoint(x: x, y: bottom)); line.line(to: NSPoint(x: x, y: top))
    line.move(to: NSPoint(x: x, y: bottom + a.height * 0.2))
    line.curve(to: NSPoint(x: x + a.width * 0.3, y: top - a.height * 0.12),
               controlPoint1: NSPoint(x: x, y: a.midY + a.height * 0.05), controlPoint2: NSPoint(x: x + a.width * 0.3, y: a.midY))
    NSColor.white.setStroke(); line.lineWidth = 22 * s; line.lineCapStyle = .round; line.stroke()
    for p in [NSPoint(x: x, y: top), NSPoint(x: x, y: bottom), NSPoint(x: x + a.width * 0.3, y: top - a.height * 0.12)] {
        let d = 34 * s
        let dot = NSBezierPath(ovalIn: NSRect(x: p.x - d, y: p.y - d, width: 2 * d, height: 2 * d))
        NSColor(red: 0.05, green: 0.07, blue: 0.09, alpha: 1).setFill(); dot.fill()
        NSColor.white.setStroke(); dot.lineWidth = 16 * s; dot.stroke()
    }
    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])!
}

for base in [16, 32, 128, 256, 512] {
    try! draw(CGFloat(base)).write(to: URL(fileURLWithPath: "\(out)/icon_\(base)x\(base).png"))
    try! draw(CGFloat(base * 2)).write(to: URL(fileURLWithPath: "\(out)/icon_\(base)x\(base)@2x.png"))
}
