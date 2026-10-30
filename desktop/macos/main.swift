// Devctl.app: ett macOS-fönster runt devctl. Appen startar den inbakade devctl-servern om den inte
// redan körs (t.ex. via `devctl autostart`) och visar webbgränssnittet i en WKWebView.
import AppKit
import WebKit

let home = FileManager.default.homeDirectoryForCurrentUser.path

/// Porten läses från ~/.config/devctl/config.json, annars 7878.
func configuredPort() -> Int {
    let path = (ProcessInfo.processInfo.environment["DEVCTL_HOME"] ?? home + "/.config/devctl") + "/config.json"
    if let data = FileManager.default.contents(atPath: path),
       let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
       let port = json["port"] as? Int {
        return port
    }
    return 7878
}

final class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKUIDelegate {
    let port = configuredPort()
    var base: URL { URL(string: "http://localhost:\(port)/")! }
    var window: NSWindow!
    var webView: WKWebView!
    var server: Process?
    var statusBar: StatusBar?

    func applicationDidFinishLaunching(_ note: Notification) {
        buildMenu()
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .default()
        webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.setValue(false, forKey: "drawsBackground") // ingen vit blixt innan sidan laddats
        if #available(macOS 13.3, *) { webView.isInspectable = true }

        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1440, height: 900),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable],
                          backing: .buffered, defer: false)
        window.title = "devctl"
        window.isReleasedWhenClosed = false // fönstret återanvänds när det öppnas igen från Dock eller menyraden
        window.backgroundColor = NSColor(red: 0.05, green: 0.07, blue: 0.09, alpha: 1)
        window.contentView = webView
        window.center()
        window.setFrameAutosaveName("devctl.main")
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)

        showMessage("Startar devctl…")
        DispatchQueue.global().async { self.ensureServer() }
        statusBar = StatusBar(port: port, app: self)
    }

    /// Visar fönstret, eventuellt på en viss flik.
    func showWindow(tab: String?) {
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        if let tab { webView.evaluateJavaScript("location.hash = '\(tab)'") }
    }

    /// Anropar devctl-API:t (samma som webbgränssnittet använder).
    func post(_ action: String, _ body: [String: Any], done: ((Bool) -> Void)? = nil) {
        var req = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/api/action")!)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("1", forHTTPHeaderField: "X-Devctl")
        var b = body
        b["action"] = action
        req.httpBody = try? JSONSerialization.data(withJSONObject: b)
        req.timeoutInterval = 120
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            let ok = (resp as? HTTPURLResponse)?.statusCode == 200
            DispatchQueue.main.async { done?(ok); self.statusBar?.fetch() }
        }.resume()
    }

    // MARK: server

    func healthy() -> Bool {
        var req = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/api/health")!)
        req.timeoutInterval = 0.7
        let sem = DispatchSemaphore(value: 0)
        var ok = false
        URLSession.shared.dataTask(with: req) { data, resp, _ in
            if let data, (resp as? HTTPURLResponse)?.statusCode == 200,
               let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               json["port"] as? Int == self.port {
                ok = true
            }
            sem.signal()
        }.resume()
        _ = sem.wait(timeout: .now() + 1.5)
        return ok
    }

    func ensureServer() {
        if !healthy() {
            guard let exe = Bundle.main.path(forResource: "devctl", ofType: nil) else {
                return fail("Hittar inte devctl i appen.")
            }
            let p = Process()
            p.executableURL = URL(fileURLWithPath: exe)
            p.arguments = ["serve"]
            // Program startade från Finder får en minimal PATH; tmux, git, docker och gh behöver mer
            var env = ProcessInfo.processInfo.environment
            let extra = ["\(home)/.local/bin", "/opt/homebrew/bin", "/usr/local/bin", "\(home)/go/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"]
            env["PATH"] = (extra + [env["PATH"] ?? ""]).joined(separator: ":")
            p.environment = env
            let logDir = home + "/.cache/devctl"
            try? FileManager.default.createDirectory(atPath: logDir, withIntermediateDirectories: true)
            FileManager.default.createFile(atPath: logDir + "/server.log", contents: nil)
            if let log = FileHandle(forWritingAtPath: logDir + "/server.log") {
                log.seekToEndOfFile()
                p.standardOutput = log
                p.standardError = log
            }
            do { try p.run() } catch { return fail("Kunde inte starta devctl: \(error.localizedDescription)") }
            server = p
            var up = false
            for _ in 0..<60 where !up {
                Thread.sleep(forTimeInterval: 0.2)
                up = healthy()
            }
            if !up { return fail("devctl startade inte. Se ~/.cache/devctl/server.log") }
        }
        DispatchQueue.main.async { self.webView.load(URLRequest(url: self.base)) }
    }

    func fail(_ msg: String) {
        DispatchQueue.main.async { self.showMessage(msg, retry: true) }
    }

    func showMessage(_ msg: String, retry: Bool = false) {
        let button = retry ? "<p><a href='devctl-retry:'>Försök igen</a></p>" : ""
        webView.loadHTMLString("""
            <html><body style="background:#0d1117;color:#8a96a6;font:15px -apple-system;display:flex;height:100vh;margin:0;align-items:center;justify-content:center;flex-direction:column">
            <p>\(msg)</p>\(button)<style>a{color:#5fb3ff}</style></body></html>
            """, baseURL: nil)
    }

    func applicationWillTerminate(_ note: Notification) {
        // Stoppa bara en server som appen själv startade
        server?.terminate()
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows: Bool) -> Bool {
        window.makeKeyAndOrderFront(nil)
        return true
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    // MARK: länkar

    /// Bara devctl-sidan visas i appen; allt annat (GitHub, Obsidian, lokala servrar) öppnas utanför.
    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url else { return decisionHandler(.cancel) }
        if url.scheme == "devctl-retry" {
            decisionHandler(.cancel)
            showMessage("Startar devctl…")
            DispatchQueue.global().async { self.ensureServer() }
            return
        }
        let own = (url.host == "localhost" || url.host == "127.0.0.1") && url.port == port
        // Inbäddade lokala sidor (t.ex. Local-Automation i Automation-fliken) laddas i sin ram
        let localFrame = (url.host == "localhost" || url.host == "127.0.0.1") && action.targetFrame?.isMainFrame == false
        if own || localFrame || url.scheme == "about" {
            return decisionHandler(.allow)
        }
        NSWorkspace.shared.open(url)
        decisionHandler(.cancel)
    }

    /// target=_blank öppnas i standardwebbläsaren.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = action.request.url { NSWorkspace.shared.open(url) }
        return nil
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) { webView.reload() }

    // MARK: menyer

    @objc func reload() { webView.reload() }
    @objc func openInBrowser() { NSWorkspace.shared.open(webView.url ?? base) }
    @objc func showTab(_ sender: NSMenuItem) {
        if let tab = sender.representedObject as? String {
            webView.evaluateJavaScript("location.hash = '\(tab)'")
        }
    }

    func buildMenu() {
        let main = NSMenu()
        func add(_ title: String, _ items: [NSMenuItem]) {
            let item = NSMenuItem()
            let menu = NSMenu(title: title)
            items.forEach(menu.addItem)
            item.submenu = menu
            main.addItem(item)
        }
        func item(_ title: String, _ action: Selector?, _ key: String, _ mods: NSEvent.ModifierFlags = .command, target: AnyObject? = nil) -> NSMenuItem {
            let i = NSMenuItem(title: title, action: action, keyEquivalent: key)
            i.keyEquivalentModifierMask = mods
            i.target = target
            return i
        }
        add("devctl", [
            item("Om devctl", #selector(NSApplication.orderFrontStandardAboutPanel(_:)), ""),
            .separator(),
            item("Göm devctl", #selector(NSApplication.hide(_:)), "h"),
            item("Göm andra", #selector(NSApplication.hideOtherApplications(_:)), "h", [.command, .option]),
            .separator(),
            item("Avsluta devctl", #selector(NSApplication.terminate(_:)), "q"),
        ])
        // Redigera-menyn behövs för att ⌘C/⌘V/⌘A ska fungera i webbvyn
        add("Redigera", [
            item("Ångra", Selector(("undo:")), "z"),
            item("Gör om", Selector(("redo:")), "z", [.command, .shift]),
            .separator(),
            item("Klipp ut", #selector(NSText.cut(_:)), "x"),
            item("Kopiera", #selector(NSText.copy(_:)), "c"),
            item("Klistra in", #selector(NSText.paste(_:)), "v"),
            item("Markera alla", #selector(NSText.selectAll(_:)), "a"),
        ])
        var view = [item("Ladda om", #selector(reload), "r", target: self),
                    item("Öppna i webbläsaren", #selector(openInBrowser), "o", [.command, .shift], target: self),
                    .separator()]
        for (i, (tab, title)) in [("overview", "Översikt"), ("tmux", "tmux"), ("servers", "Servrar"),
                                  ("docker", "Docker"), ("orchestrator", "Orkestrator"), ("automation", "Automation")].enumerated() {
            let m = item(title, #selector(showTab(_:)), "\(i + 1)", target: self)
            m.representedObject = tab
            view.append(m)
        }
        add("Visa", view)
        add("Fönster", [
            item("Minimera", #selector(NSWindow.performMiniaturize(_:)), "m"),
            item("Zooma", #selector(NSWindow.performZoom(_:)), ""),
            item("Stäng", #selector(NSWindow.performClose(_:)), "w"),
        ])
        NSApp.mainMenu = main
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()
