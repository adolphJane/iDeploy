import AppKit
import SwiftUI

final class NativeAppDelegate: NSObject, NSApplicationDelegate {
    private var backend: Process?
    private var statusItem: NSStatusItem?
    var showWindow: (() -> Void)?

    func applicationDidFinishLaunching(_ notification: Notification) {
        ideploy_start_app_updater()
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        item.button?.image = NSImage(systemSymbolName: "shippingbox", accessibilityDescription: "iDeploy")
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "打开 iDeploy", action: #selector(openWindow), keyEquivalent: ""))
        menu.addItem(.separator())
        menu.addItem(NSMenuItem(title: "退出 iDeploy", action: #selector(quit), keyEquivalent: ""))
        for entry in menu.items { entry.target = self }
        item.menu = menu
        statusItem = item

        guard let executable = Bundle.main.executableURL?
            .deletingLastPathComponent().appendingPathComponent("iDeployBackend"),
            FileManager.default.isExecutableFile(atPath: executable.path) else { return }
        let process = Process()
        process.executableURL = executable
        process.arguments = ["native-backend", "\(ProcessInfo.processInfo.processIdentifier)"]
        do {
            try process.run()
            backend = process
        } catch {
            NSLog("Failed to start iDeploy backend: %@", error.localizedDescription)
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        if backend?.isRunning == true { backend?.terminate() }
    }

    @objc private func openWindow() {
        NSApp.activate(ignoringOtherApps: true)
        if let window = NSApp.windows.first(where: { $0.canBecomeKey && !$0.isVisible }) {
            window.makeKeyAndOrderFront(nil)
        } else if NSApp.windows.contains(where: { $0.canBecomeKey && $0.isVisible }) {
            NSApp.windows.first(where: { $0.canBecomeKey && $0.isVisible })?.makeKeyAndOrderFront(nil)
        } else {
            showWindow?()
        }
    }

    @objc private func quit() { NSApp.terminate(nil) }
}

@main
struct IDeployApp: App {
    @NSApplicationDelegateAdaptor(NativeAppDelegate.self) private var appDelegate
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup("iDeploy", id: "main") {
            ContentView()
                .environmentObject(model)
                .frame(minWidth: 1000, minHeight: 670)
                .task {
                    await model.start()
                    while !Task.isCancelled {
                        try? await Task.sleep(nanoseconds: 10_000_000_000)
                        if !model.busy { await model.refresh() }
                    }
                }
        }
        .defaultSize(width: 1260, height: 800)
        .commands {
            CommandGroup(replacing: .newItem) { }
            CommandMenu("操作") {
                Button("刷新") { Task { await model.refresh() } }
                    .keyboardShortcut("r")
                Divider()
                Button("总览") { model.section = .overview }.keyboardShortcut("1")
                Button("部署") { model.section = .deployments }.keyboardShortcut("2")
                Button("服务器") { model.section = .servers }.keyboardShortcut("3")
                Button("项目") { model.section = .projects }.keyboardShortcut("4")
            }
        }
        Settings {
            SettingsPage().environmentObject(model)
                .frame(minWidth: 740, minHeight: 600)
        }
    }
}
