// Ikon i menyraden: visar hur många branches som har något igång och ger snabb åtkomst till
// servrar, worktrees, orkestratorn och dagens logg. Läser samma /api/state som webbgränssnittet.
import AppKit

/// Ett menyval som kör en closure.
final class MenuAction: NSObject {
    let fn: () -> Void
    init(_ fn: @escaping () -> Void) { self.fn = fn }
    @objc func run(_ sender: Any?) { fn() }
}

final class StatusBar: NSObject, NSMenuDelegate {
    let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    let port: Int
    weak var app: AppDelegate?
    var state: [String: Any] = [:]
    var actions: [MenuAction] = []
    var timer: Timer?

    init(port: Int, app: AppDelegate) {
        self.port = port
        self.app = app
        super.init()
        if let button = item.button {
            button.image = StatusBar.glyph()
            button.imagePosition = .imageLeading
            button.toolTip = "devctl"
        }
        let menu = NSMenu()
        menu.delegate = self
        menu.autoenablesItems = false
        item.menu = menu
        timer = Timer.scheduledTimer(withTimeInterval: 5, repeats: true) { [weak self] _ in self?.fetch() }
        fetch()
    }

    /// Tre panes som i en tmux-layout, ritade som mallbild så att färgen följer menyraden.
    static func glyph() -> NSImage {
        let img = NSImage(size: NSSize(width: 18, height: 18), flipped: false) { _ in
            let r = NSRect(x: 1.5, y: 2.5, width: 15, height: 13)
            let outline = NSBezierPath(roundedRect: r, xRadius: 2.5, yRadius: 2.5)
            outline.lineWidth = 1.5
            NSColor.black.setStroke()
            outline.stroke()
            let lines = NSBezierPath()
            lines.move(to: NSPoint(x: 9.5, y: r.minY)); lines.line(to: NSPoint(x: 9.5, y: r.maxY))
            lines.move(to: NSPoint(x: 9.5, y: r.midY)); lines.line(to: NSPoint(x: r.maxX, y: r.midY))
            lines.lineWidth = 1.5
            lines.stroke()
            return true
        }
        img.isTemplate = true
        return img
    }

    // MARK: data

    func fetch() {
        var req = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/api/state")!)
        req.timeoutInterval = 4
        URLSession.shared.dataTask(with: req) { [weak self] data, _, _ in
            guard let self, let data, let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                DispatchQueue.main.async { self?.item.button?.title = ""; self?.item.button?.toolTip = "devctl: servern svarar inte" }
                return
            }
            DispatchQueue.main.async { self.state = json; self.updateButton() }
        }.resume()
    }

    func list(_ v: Any?) -> [[String: Any]] { v as? [[String: Any]] ?? [] }

    struct Busy {
        let repo: String, branch: String, path: String
        let panes: Int, containers: Int
        let servers: [[String: Any]]
    }

    /// Worktrees med något igång (panes, servrar eller containrar), flest saker först.
    func busy() -> [Busy] {
        let servers = list(state["servers"])
        var out: [Busy] = []
        for repo in list(state["repos"]) {
            for wt in list(repo["worktrees"]) {
                let ports = (wt["servers"] as? [Int]) ?? []
                let panes = ((wt["panes"] as? [Any]) ?? []).count
                let cts = ((wt["containers"] as? [Any]) ?? []).count
                if panes + ports.count + cts == 0 { continue }
                let branch = (wt["branch"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "(\(wt["head"] as? String ?? "?"))"
                out.append(Busy(repo: repo["name"] as? String ?? "?", branch: branch, path: wt["path"] as? String ?? "",
                                panes: panes, containers: cts,
                                servers: servers.filter { ports.contains($0["port"] as? Int ?? -1) }))
            }
        }
        return out.sorted { ($0.servers.count + $0.panes + $0.containers) > ($1.servers.count + $1.panes + $1.containers) }
    }

    var devServers: [[String: Any]] {
        list(state["servers"]).filter { !($0["app"] as? Bool ?? false) && ($0["kind"] as? String) != "docker" }
    }
    var runningContainers: Int {
        list((state["docker"] as? [String: Any])?["containers"]).filter { ($0["state"] as? String) == "running" }.count
    }

    func updateButton() {
        let n = busy().count
        item.button?.title = n > 0 ? " \(n)" : ""
        let orch = state["orchestrator"] is [String: Any] ? " · orkestrator igång" : ""
        item.button?.toolTip = "devctl: \(n) branches med något igång · \(devServers.count) servrar · \(runningContainers) containrar\(orch)"
    }

    // MARK: meny

    func add(_ menu: NSMenu, _ title: String, key: String = "", enabled: Bool = true, indent: Int = 0, _ fn: (() -> Void)? = nil) -> NSMenuItem {
        let i = NSMenuItem(title: title, action: nil, keyEquivalent: key)
        i.indentationLevel = indent
        if let fn {
            let a = MenuAction(fn)
            actions.append(a)
            i.target = a
            i.action = #selector(MenuAction.run(_:))
        }
        i.isEnabled = enabled && fn != nil
        menu.addItem(i)
        return i
    }

    func header(_ menu: NSMenu, _ title: String) {
        let i = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        i.attributedTitle = NSAttributedString(string: title, attributes: [
            .font: NSFont.systemFont(ofSize: 11, weight: .semibold), .foregroundColor: NSColor.secondaryLabelColor])
        i.isEnabled = false
        menu.addItem(i)
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        fetch()
        menu.removeAllItems()
        actions.removeAll()
        guard let app else { return }

        if state.isEmpty {
            _ = add(menu, "Servern svarar inte", enabled: false)
            _ = add(menu, "Öppna devctl") { app.showWindow(tab: nil) }
            menu.addItem(.separator())
            _ = add(menu, "Avsluta devctl", key: "q") { NSApp.terminate(nil) }
            return
        }

        let b = busy()
        header(menu, "\(b.count) branches igång · \(devServers.count) servrar · \(runningContainers) containrar")
        if let o = state["orchestrator"] as? [String: Any] {
            _ = add(menu, "🎛 Orkestrator igång (\(o["pane"] as? String ?? ""))") { app.showWindow(tab: "orchestrator") }
        } else {
            _ = add(menu, "🎛 Starta orkestratorn") {
                app.post("orchestrator_start", [:]) { _ in app.showWindow(tab: "orchestrator") }
            }
        }

        menu.addItem(.separator())
        header(menu, b.isEmpty ? "Inget igång i dina repos" : "Igång")
        for w in b.prefix(12) {
            let parts = [w.servers.isEmpty ? nil : "\(w.servers.count) server\(w.servers.count == 1 ? "" : "ar")",
                         w.panes == 0 ? nil : "\(w.panes) pane\(w.panes == 1 ? "" : "s")",
                         w.containers == 0 ? nil : "\(w.containers) 🐳"].compactMap { $0 }
            let top = add(menu, "\(w.repo) @ \(w.branch)   \(parts.joined(separator: " · "))") { app.showWindow(tab: "overview") }
            let sub = NSMenu()
            sub.autoenablesItems = false
            for s in w.servers.prefix(15) {
                let port = s["port"] as? Int ?? 0
                _ = add(sub, ":\(port)  \(s["kind"] as? String ?? "")  \(s["process"] as? String ?? "")") {
                    NSWorkspace.shared.open(URL(string: "http://localhost:\(port)")!)
                }
            }
            if !w.servers.isEmpty { sub.addItem(.separator()) }
            _ = add(sub, "Ny terminal här") { app.post("open_worktree", ["path": w.path]) { _ in app.showWindow(tab: "tmux") } }
            _ = add(sub, "Öppna i editorn") { app.post("editor", ["path": w.path]) }
            if !w.branch.hasPrefix("(") {
                _ = add(sub, "Anteckning i Obsidian") { app.post("note", ["repo": w.path, "branch": w.branch, "open": true]) }
            }
            _ = add(sub, "Visa i Finder") { NSWorkspace.shared.open(URL(fileURLWithPath: w.path)) }
            top.submenu = sub
        }

        let loose = devServers.filter { $0["worktree"] == nil || $0["worktree"] is NSNull }
        if !loose.isEmpty {
            menu.addItem(.separator())
            header(menu, "Andra servrar")
            for s in loose.prefix(8) {
                let port = s["port"] as? Int ?? 0
                _ = add(menu, ":\(port)  \(s["kind"] as? String ?? "")  \(s["process"] as? String ?? "")") {
                    NSWorkspace.shared.open(URL(string: "http://localhost:\(port)")!)
                }
            }
        }

        if let a = state["automation"] as? [String: Any], (a["url"] as? String ?? "") != "" {
            menu.addItem(.separator())
            let tasks = list(a["tasks"])
            if a["online"] as? Bool == true {
                let running = tasks.filter { $0["running"] as? Bool == true }.compactMap { $0["name"] as? String }
                let failed = tasks.filter { t in
                    if let last = t["lastRun"] as? [String: Any], let exit = last["exit"] as? Int { return exit != 0 }
                    return false
                }.compactMap { $0["name"] as? String }
                var title = "⚙︎ Local Automation · \(tasks.count) uppgifter"
                if !running.isEmpty { title += " · kör: " + running.joined(separator: ", ") }
                if !failed.isEmpty { title += " · ✗ " + failed.joined(separator: ", ") }
                _ = add(menu, title) { app.showWindow(tab: "automation") }
            } else {
                _ = add(menu, "⚙︎ Local Automation svarar inte") { app.showWindow(tab: "automation") }
            }
        }

        menu.addItem(.separator())
        _ = add(menu, "Öppna devctl", key: "o") { app.showWindow(tab: nil) }
        _ = add(menu, "Fråga orkestratorn …") { app.quickChat() }
        _ = add(menu, "Servrar") { app.showWindow(tab: "servers") }
        _ = add(menu, "Docker (\(runningContainers) igång)") { app.showWindow(tab: "docker") }
        _ = add(menu, "Dagens logg i Obsidian") { app.post("daily_log", ["open": true]) }
        menu.addItem(.separator())
        _ = add(menu, "Avsluta devctl", key: "q") { NSApp.terminate(nil) }
    }
}
