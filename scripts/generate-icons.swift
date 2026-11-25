#!/usr/bin/env swift
import AppKit
import CoreGraphics
import Foundation

struct RGB {
    let r: CGFloat
    let g: CGFloat
    let b: CGFloat
    let a: CGFloat

    init(hex: UInt32, alpha: CGFloat = 1.0) {
        self.r = CGFloat((hex >> 16) & 0xFF) / 255.0
        self.g = CGFloat((hex >> 8) & 0xFF) / 255.0
        self.b = CGFloat(hex & 0xFF) / 255.0
        self.a = alpha
    }

    var cgColor: CGColor {
        CGColor(srgbRed: r, green: g, blue: b, alpha: a)
    }
}

enum Variant {
    case light
    case dark
    case tinted
    case monochrome
    case splash
}

struct PebbleSpec {
    static let n = 4
    static let circ: CGFloat = 49.0
    static let rot0: CGFloat = .pi / 4.0
    static let sx: CGFloat = 1.02
    static let corner: CGFloat = 0.40
    static let asym: CGFloat = 0.35
    static let verts: [(s: CGFloat, d: CGFloat)] = [
        (0.97, -0.04),
        (1.06,  0.05),
        (0.94, -0.03),
        (1.03,  0.04),
    ]
    static let bodyRotDeg: CGFloat = -8.0
    static let eyeW: CGFloat = 5.0
    static let eyeH: CGFloat = 9.5
    static let eyeGap: CGFloat = 19.0
    static let eyeEx: CGFloat = 54.5
    static let eyeEy: CGFloat = 46.0
    static let eyeLeanDeg: CGFloat = -14.0

    static func polygonVertices() -> [CGPoint] {
        var pts: [CGPoint] = []
        for i in 0..<n {
            let v = verts[i]
            let a = rot0 + (CGFloat(i) / CGFloat(n)) * .pi * 2.0 + v.d * asym * 0.5
            let rr = circ * (1.0 + (v.s - 1.0) * asym)
            pts.append(CGPoint(x: cos(a) * rr * sx, y: (sin(a) * rr) / sx))
        }
        let minX = pts.map(\.x).min()!
        let maxX = pts.map(\.x).max()!
        let minY = pts.map(\.y).min()!
        let maxY = pts.map(\.y).max()!
        let ox = 50.0 - (minX + maxX) / 2.0
        let oy = 50.0 - (minY + maxY) / 2.0
        return pts.map { CGPoint(x: $0.x + ox, y: $0.y + oy) }
    }

    static func cut(_ a: CGPoint, _ b: CGPoint, _ k: CGFloat) -> CGPoint {
        CGPoint(x: a.x + (b.x - a.x) * k, y: a.y + (b.y - a.y) * k)
    }

    static func bodyCGPath() -> CGPath {
        let V = polygonVertices()
        let path = CGMutablePath()
        for i in 0..<V.count {
            let P = V[(i - 1 + V.count) % V.count]
            let v = V[i]
            let N = V[(i + 1) % V.count]
            let start = cut(v, P, corner)
            let end = cut(v, N, corner)
            if i == 0 {
                path.move(to: start)
            } else {
                path.addLine(to: start)
            }
            path.addQuadCurve(to: end, control: v)
        }
        path.closeSubpath()
        return path
    }

    static func bodySVGPathD() -> String {
        let V = polygonVertices()
        func fmt(_ p: CGPoint) -> String {
            String(format: "%.2f %.2f", Double(p.x), Double(p.y))
        }
        var d = ""
        for i in 0..<V.count {
            let P = V[(i - 1 + V.count) % V.count]
            let v = V[i]
            let N = V[(i + 1) % V.count]
            d += (i == 0 ? "M" : "L") + fmt(cut(v, P, corner)) + "Q" + fmt(v) + " " + fmt(cut(v, N, corner))
        }
        return d + "Z"
    }
}

func makeBitmapContext(size: Int) -> (NSBitmapImageRep, CGContext) {
    let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: size,
        pixelsHigh: size,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    )!
    rep.size = NSSize(width: size, height: size)
    let ctx = NSGraphicsContext(bitmapImageRep: rep)!.cgContext
    ctx.setAllowsAntialiasing(true)
    ctx.setShouldAntialias(true)
    ctx.interpolationQuality = .high
    // Flip to y-down SVG coordinate space
    ctx.translateBy(x: 0, y: CGFloat(size))
    ctx.scaleBy(x: 1, y: -1)
    return (rep, ctx)
}

func drawLinearGradient(
    ctx: CGContext,
    colors: [CGColor],
    locations: [CGFloat],
    start: CGPoint,
    end: CGPoint
) {
    let space = CGColorSpace(name: CGColorSpace.sRGB)!
    let grad = CGGradient(colorsSpace: space, colors: colors as CFArray, locations: locations)!
    ctx.drawLinearGradient(grad, start: start, end: end, options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
}

func drawBackground(ctx: CGContext, rect: CGRect, dark: Bool, tinted: Bool = false) {
    if tinted {
        ctx.setFillColor(RGB(hex: 0x171717).cgColor)
        ctx.fill(rect)
        return
    }
    if dark {
        drawLinearGradient(
            ctx: ctx,
            colors: [RGB(hex: 0x1E1E1E).cgColor, RGB(hex: 0x141414).cgColor, RGB(hex: 0x0D0D0D).cgColor],
            locations: [0.0, 0.5, 1.0],
            start: CGPoint(x: rect.minX, y: rect.minY),
            end: CGPoint(x: rect.maxX, y: rect.maxY)
        )
    } else {
        ctx.setFillColor(RGB(hex: 0xFFFFFF).cgColor)
        ctx.fill(rect)
    }
}

func drawBot(ctx: CGContext, canvasSize: CGFloat, heroFraction: CGFloat, variant: Variant, withShadow: Bool) {
    let scale = (canvasSize * heroFraction) / 100.0
    let offset = (canvasSize - 100.0 * scale) / 2.0

    ctx.saveGState()
    ctx.translateBy(x: offset, y: offset)
    ctx.scaleBy(x: scale, y: scale)

    let bodyPath = PebbleSpec.bodyCGPath()

    // Soft ground shadow under bot
    if withShadow {
        ctx.saveGState()
        ctx.translateBy(x: 50, y: 52.8)
        ctx.rotate(by: PebbleSpec.bodyRotDeg * .pi / 180.0)
        ctx.translateBy(x: -50, y: -50)
        let shadowColor: CGColor
        switch variant {
        case .dark:
            shadowColor = RGB(hex: 0x000000, alpha: 0.45).cgColor
        case .light:
            shadowColor = RGB(hex: 0x171717, alpha: 0.12).cgColor
        default:
            shadowColor = RGB(hex: 0x000000, alpha: 0.25).cgColor
        }
        ctx.setShadow(offset: CGSize(width: 0, height: -2.5 * scale), blur: 5.5 * scale, color: shadowColor)
        ctx.addPath(bodyPath)
        ctx.setFillColor(RGB(hex: 0x7AAA98).cgColor)
        ctx.fillPath()
        ctx.restoreGState()
    }

    // Bot body
    ctx.saveGState()
    ctx.translateBy(x: 50, y: 50)
    ctx.rotate(by: PebbleSpec.bodyRotDeg * .pi / 180.0)
    ctx.translateBy(x: -50, y: -50)

    ctx.addPath(bodyPath)
    ctx.clip()

    switch variant {
    case .light, .splash:
        drawLinearGradient(
            ctx: ctx,
            colors: [RGB(hex: 0x83B4A2).cgColor, RGB(hex: 0x7AAA98).cgColor, RGB(hex: 0x6D9E8B).cgColor],
            locations: [0.0, 0.55, 1.0],
            start: CGPoint(x: 15, y: 12),
            end: CGPoint(x: 85, y: 88)
        )
    case .dark:
        drawLinearGradient(
            ctx: ctx,
            colors: [RGB(hex: 0x86B9A6).cgColor, RGB(hex: 0x7AAA98).cgColor, RGB(hex: 0x699A87).cgColor],
            locations: [0.0, 0.55, 1.0],
            start: CGPoint(x: 15, y: 12),
            end: CGPoint(x: 85, y: 88)
        )
    case .tinted:
        drawLinearGradient(
            ctx: ctx,
            colors: [RGB(hex: 0xE6E6E6).cgColor, RGB(hex: 0xCFCFCF).cgColor],
            locations: [0.0, 1.0],
            start: CGPoint(x: 15, y: 12),
            end: CGPoint(x: 85, y: 88)
        )
    case .monochrome:
        ctx.setFillColor(RGB(hex: 0xFFFFFF).cgColor)
        ctx.fill(CGRect(x: 0, y: 0, width: 100, height: 100))
    }
    ctx.restoreGState()

    // Eyes
    let eyeCxs = [PebbleSpec.eyeEx - PebbleSpec.eyeGap / 2.0, PebbleSpec.eyeEx + PebbleSpec.eyeGap / 2.0]
    let w = PebbleSpec.eyeW * 2.0
    let h = PebbleSpec.eyeH * 2.0
    let rx = min(PebbleSpec.eyeW, PebbleSpec.eyeH)

    for cx in eyeCxs {
        ctx.saveGState()
        ctx.translateBy(x: cx, y: PebbleSpec.eyeEy)
        ctx.rotate(by: PebbleSpec.eyeLeanDeg * .pi / 180.0)
        ctx.translateBy(x: -cx, y: -PebbleSpec.eyeEy)

        let eyeRect = CGRect(x: cx - PebbleSpec.eyeW, y: PebbleSpec.eyeEy - PebbleSpec.eyeH, width: w, height: h)
        let eyePath = CGPath(roundedRect: eyeRect, cornerWidth: rx, cornerHeight: rx, transform: nil)
        ctx.addPath(eyePath)

        switch variant {
        case .light, .splash:
            ctx.setFillColor(RGB(hex: 0x1C140E, alpha: 0.90).cgColor)
            ctx.fillPath()
        case .dark:
            ctx.setFillColor(RGB(hex: 0x14100D, alpha: 0.92).cgColor)
            ctx.fillPath()
        case .tinted:
            ctx.setFillColor(RGB(hex: 0x171717).cgColor)
            ctx.fillPath()
        case .monochrome:
            ctx.setBlendMode(.clear)
            ctx.fillPath()
        }
        ctx.restoreGState()
    }

    ctx.restoreGState()
}

func savePNG(_ rep: NSBitmapImageRep, to path: String) throws {
    let url = URL(fileURLWithPath: path)
    try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
    guard let data = rep.representation(using: .png, properties: [:]) else {
        fatalError("Failed to encode PNG: \(path)")
    }
    try data.write(to: url)
}

func renderFullBleedIcon(size: Int, variant: Variant, heroFraction: CGFloat = 0.82, circleMask: Bool = false) -> NSBitmapImageRep {
    let (rep, ctx) = makeBitmapContext(size: size)
    let rect = CGRect(x: 0, y: 0, width: size, height: size)

    if circleMask {
        ctx.addEllipse(in: rect)
        ctx.clip()
    }

    switch variant {
    case .light:
        drawBackground(ctx: ctx, rect: rect, dark: false)
        drawBot(ctx: ctx, canvasSize: CGFloat(size), heroFraction: heroFraction, variant: .light, withShadow: true)
    case .dark:
        drawBackground(ctx: ctx, rect: rect, dark: true)
        drawBot(ctx: ctx, canvasSize: CGFloat(size), heroFraction: heroFraction, variant: .dark, withShadow: true)
    case .tinted:
        drawBackground(ctx: ctx, rect: rect, dark: true, tinted: true)
        drawBot(ctx: ctx, canvasSize: CGFloat(size), heroFraction: heroFraction, variant: .tinted, withShadow: false)
    case .monochrome:
        drawBot(ctx: ctx, canvasSize: CGFloat(size), heroFraction: heroFraction, variant: .monochrome, withShadow: false)
    case .splash:
        drawBot(ctx: ctx, canvasSize: CGFloat(size), heroFraction: heroFraction, variant: .splash, withShadow: false)
    }

    return rep
}

func renderMacOSSquircleIcon(size: Int) -> NSBitmapImageRep {
    let (rep, ctx) = makeBitmapContext(size: size)
    let s = CGFloat(size)
    // Apple HIG macOS squircle metrics: 824x824 card in 1024x1024 canvas, corner radius ~185
    let cardSize = s * (824.0 / 1024.0)
    let cardPad = (s - cardSize) / 2.0
    let cornerR = cardSize * (185.0 / 824.0)
    let cardRect = CGRect(x: cardPad, y: cardPad, width: cardSize, height: cardSize)
    let cardPath = CGPath(roundedRect: cardRect, cornerWidth: cornerR, cornerHeight: cornerR, transform: nil)

    // Subtle dock drop shadow
    ctx.saveGState()
    ctx.setShadow(
        offset: CGSize(width: 0, height: -s * (10.0 / 1024.0)),
        blur: s * (22.0 / 1024.0),
        color: RGB(hex: 0x000000, alpha: 0.22).cgColor
    )
    ctx.addPath(cardPath)
    ctx.setFillColor(RGB(hex: 0xFFFFFF).cgColor)
    ctx.fillPath()
    ctx.restoreGState()

    // Clip to squircle and draw ceramic background + celadon bot
    ctx.saveGState()
    ctx.addPath(cardPath)
    ctx.clip()
    drawBackground(ctx: ctx, rect: cardRect, dark: false)

    ctx.translateBy(x: cardPad, y: cardPad)
    drawBot(ctx: ctx, canvasSize: cardSize, heroFraction: 0.82, variant: .light, withShadow: true)
    ctx.restoreGState()

    // Crisp subtle inner edge highlight
    ctx.saveGState()
    ctx.addPath(cardPath)
    ctx.setStrokeColor(RGB(hex: 0x171717, alpha: 0.08).cgColor)
    ctx.setLineWidth(max(1.0, s / 512.0))
    ctx.strokePath()
    ctx.restoreGState()

    return rep
}

func renderBackgroundOnly(size: Int, dark: Bool) -> NSBitmapImageRep {
    let (rep, ctx) = makeBitmapContext(size: size)
    drawBackground(ctx: ctx, rect: CGRect(x: 0, y: 0, width: size, height: size), dark: dark)
    return rep
}

enum MenuBarPebbleSpec {
    static let eyeW: CGFloat = 5.6
    static let eyeH: CGFloat = 10.4
    static let eyeGap: CGFloat = 20.8
    static let eyeEx: CGFloat = 53.8
    static let eyeEy: CGFloat = 46.2
    static let eyeLeanDeg: CGFloat = -14.0
}

func renderMenuBarIcon(size: Int, bodyFraction: CGFloat, template: Bool) -> NSBitmapImageRep {
    let (rep, ctx) = makeBitmapContext(size: size)
    let s = CGFloat(size)

    var rot = CGAffineTransform(translationX: 50, y: 50)
        .rotated(by: PebbleSpec.bodyRotDeg * .pi / 180.0)
        .translatedBy(x: -50, y: -50)
    let rawBody = PebbleSpec.bodyCGPath()
    let rotatedBody = rawBody.copy(using: &rot) ?? rawBody
    let bbox = rotatedBody.boundingBoxOfPath
    let scale = (s * bodyFraction) / max(bbox.width, bbox.height)

    ctx.saveGState()
    ctx.translateBy(x: s / 2.0, y: s / 2.0)
    ctx.scaleBy(x: scale, y: scale)
    ctx.translateBy(x: -bbox.midX, y: -bbox.midY)

    // Body
    ctx.saveGState()
    ctx.addPath(rotatedBody)
    ctx.clip()
    if template {
        ctx.setFillColor(RGB(hex: 0x000000).cgColor)
        ctx.fill(bbox.insetBy(dx: -2, dy: -2))
    } else {
        drawLinearGradient(
            ctx: ctx,
            colors: [RGB(hex: 0x83B4A2).cgColor, RGB(hex: 0x7AAA98).cgColor, RGB(hex: 0x6D9E8B).cgColor],
            locations: [0.0, 0.55, 1.0],
            start: CGPoint(x: 15, y: 12),
            end: CGPoint(x: 85, y: 88)
        )
    }
    ctx.restoreGState()

    // Optically compensated eyes
    let eyeCxs = [
        MenuBarPebbleSpec.eyeEx - MenuBarPebbleSpec.eyeGap / 2.0,
        MenuBarPebbleSpec.eyeEx + MenuBarPebbleSpec.eyeGap / 2.0,
    ]
    let w = MenuBarPebbleSpec.eyeW * 2.0
    let h = MenuBarPebbleSpec.eyeH * 2.0
    let rx = min(MenuBarPebbleSpec.eyeW, MenuBarPebbleSpec.eyeH)

    for cx in eyeCxs {
        ctx.saveGState()
        ctx.translateBy(x: cx, y: MenuBarPebbleSpec.eyeEy)
        ctx.rotate(by: MenuBarPebbleSpec.eyeLeanDeg * .pi / 180.0)
        ctx.translateBy(x: -cx, y: -MenuBarPebbleSpec.eyeEy)

        let eyeRect = CGRect(
            x: cx - MenuBarPebbleSpec.eyeW,
            y: MenuBarPebbleSpec.eyeEy - MenuBarPebbleSpec.eyeH,
            width: w,
            height: h
        )
        let eyePath = CGPath(roundedRect: eyeRect, cornerWidth: rx, cornerHeight: rx, transform: nil)
        ctx.addPath(eyePath)
        if template {
            ctx.setBlendMode(.clear)
            ctx.fillPath()
        } else {
            ctx.setFillColor(RGB(hex: 0x1C140E, alpha: 0.92).cgColor)
            ctx.fillPath()
        }
        ctx.restoreGState()
    }

    ctx.restoreGState()
    return rep
}

func generateMenuBarAssets(root: String) throws {
    let menuBarImagesetDir = "\(root)/apps/apple/Pebble/Assets.xcassets/MenuBarIcon.imageset"
    try FileManager.default.createDirectory(atPath: menuBarImagesetDir, withIntermediateDirectories: true)

    let menuBarFraction: CGFloat = 15.5 / 18.0
    try savePNG(renderMenuBarIcon(size: 18, bodyFraction: menuBarFraction, template: true), to: "\(menuBarImagesetDir)/menubar@1x.png")
    try savePNG(renderMenuBarIcon(size: 36, bodyFraction: menuBarFraction, template: true), to: "\(menuBarImagesetDir)/menubar@2x.png")
    try savePNG(renderMenuBarIcon(size: 54, bodyFraction: menuBarFraction, template: true), to: "\(menuBarImagesetDir)/menubar@3x.png")

    let contentsJSON = """
    {
      "images" : [
        {
          "filename" : "menubar@1x.png",
          "idiom" : "universal",
          "scale" : "1x"
        },
        {
          "filename" : "menubar@2x.png",
          "idiom" : "universal",
          "scale" : "2x"
        },
        {
          "filename" : "menubar@3x.png",
          "idiom" : "universal",
          "scale" : "3x"
        }
      ],
      "info" : {
        "author" : "xcode",
        "version" : 1
      },
      "properties" : {
        "template-rendering-intent" : "template"
      }
    }
    """
    try contentsJSON.write(toFile: "\(menuBarImagesetDir)/Contents.json", atomically: true, encoding: .utf8)

    let desktopAssetsDir = "\(root)/cmd/bot-hub-desktop/assets"
    try FileManager.default.createDirectory(atPath: desktopAssetsDir, withIntermediateDirectories: true)
    try savePNG(renderMenuBarIcon(size: 44, bodyFraction: 15.5 / 22.0, template: true), to: "\(desktopAssetsDir)/tray_mac_template.png")
    try savePNG(renderMenuBarIcon(size: 64, bodyFraction: 0.86, template: false), to: "\(desktopAssetsDir)/tray_color.png")
}

// MARK: - Paths
let root = FileManager.default.currentDirectoryPath
let menubarOnly = CommandLine.arguments.contains("--menubar-only")

try generateMenuBarAssets(root: root)
if menubarOnly {
    print("Generated menu bar / tray icon assets successfully.")
    exit(0)
}
let appleIconDir = "\(root)/apps/apple/Pebble/Assets.xcassets/AppIcon.appiconset"
let androidDrawableDir = "\(root)/apps/android/app/src/main/res/drawable-nodpi"
let webPublicDir = "\(root)/web/public"
let macIconsetDir = "/tmp/pebble-AppIcon.iconset"

// 1. Apple AppIcon.appiconset
try savePNG(renderFullBleedIcon(size: 1024, variant: .light), to: "\(appleIconDir)/icon.png")
try savePNG(renderFullBleedIcon(size: 1024, variant: .dark), to: "\(appleIconDir)/icon-dark.png")
try savePNG(renderFullBleedIcon(size: 1024, variant: .tinted), to: "\(appleIconDir)/icon-tinted.png")

// 2. macOS iconset
try? FileManager.default.removeItem(atPath: macIconsetDir)
for (pt, scale, name) in [
    (16, 1, "icon_16x16.png"),
    (16, 2, "icon_16x16@2x.png"),
    (32, 1, "icon_32x32.png"),
    (32, 2, "icon_32x32@2x.png"),
    (128, 1, "icon_128x128.png"),
    (128, 2, "icon_128x128@2x.png"),
    (256, 1, "icon_256x256.png"),
    (256, 2, "icon_256x256@2x.png"),
    (512, 1, "icon_512x512.png"),
    (512, 2, "icon_512x512@2x.png"),
] {
    try savePNG(renderMacOSSquircleIcon(size: pt * scale), to: "\(macIconsetDir)/\(name)")
}

// 3. Web public icons + SVG favicon
try savePNG(renderFullBleedIcon(size: 180, variant: .light), to: "\(webPublicDir)/apple-touch-icon.png")
try savePNG(renderFullBleedIcon(size: 64, variant: .light), to: "\(webPublicDir)/favicon.png")

let pathD = PebbleSpec.bodySVGPathD()
let eyeCxL = PebbleSpec.eyeEx - PebbleSpec.eyeGap / 2.0
let eyeCxR = PebbleSpec.eyeEx + PebbleSpec.eyeGap / 2.0
let eyeY = PebbleSpec.eyeEy - PebbleSpec.eyeH
let svg = """
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
  <style>
    .bg { fill: #ffffff; }
    .eye { fill: rgba(28, 20, 14, 0.90); }
    @media (prefers-color-scheme: dark) {
      .bg { fill: #171717; }
      .eye { fill: rgba(20, 16, 13, 0.92); }
    }
  </style>
  <defs>
    <linearGradient id="celadon" x1="15%" y1="12%" x2="85%" y2="88%">
      <stop offset="0%" stop-color="#83b4a2" />
      <stop offset="55%" stop-color="#7aaa98" />
      <stop offset="100%" stop-color="#6d9e8b" />
    </linearGradient>
  </defs>
  <rect class="bg" width="100" height="100" rx="24" />
  <g transform="translate(50 50) scale(0.84) translate(-50 -50)">
    <g transform="rotate(\(PebbleSpec.bodyRotDeg) 50 50)">
      <path d="\(pathD)" fill="url(#celadon)" />
    </g>
    <rect class="eye" x="\(eyeCxL - PebbleSpec.eyeW)" y="\(eyeY)" width="\(PebbleSpec.eyeW * 2)" height="\(PebbleSpec.eyeH * 2)" rx="\(PebbleSpec.eyeW)" transform="rotate(\(PebbleSpec.eyeLeanDeg) \(eyeCxL) \(PebbleSpec.eyeEy))" />
    <rect class="eye" x="\(eyeCxR - PebbleSpec.eyeW)" y="\(eyeY)" width="\(PebbleSpec.eyeW * 2)" height="\(PebbleSpec.eyeH * 2)" rx="\(PebbleSpec.eyeW)" transform="rotate(\(PebbleSpec.eyeLeanDeg) \(eyeCxR) \(PebbleSpec.eyeEy))" />
  </g>
</svg>
"""
try FileManager.default.createDirectory(atPath: webPublicDir, withIntermediateDirectories: true)
try svg.write(toFile: "\(webPublicDir)/favicon.svg", atomically: true, encoding: .utf8)

// 4. Android adaptive icon background (1024x1024 pure white)
try savePNG(renderBackgroundOnly(size: 1024, dark: false), to: "\(androidDrawableDir)/ic_launcher_background_img.png")

print("Generated PNG/SVG assets successfully. pathD=\(pathD)")
