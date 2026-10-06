import AppKit
import SwiftUI
import Combine

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {

    var store = TaskStore()
    var panel: FloatingPanel!
    var statusItem: NSStatusItem!
    var popover: NSPopover?
    private var widgetHosting: NSView?
    private var taskObservation: AnyCancellable?

    func applicationDidFinishLaunching(_ notification: Notification) {
        store.loadState()

        setupPanel()
        setupStatusItem()

        // Auto-open task selection if no active task on first launch
        if store.currentTaskName == nil {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { [weak self] in
                self?.showTaskSelection(completingPrevious: false)
            }
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        store.writeClosingRowOnTerminate()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }

    // MARK: - Panel

    private func setupPanel() {
        panel = FloatingPanel(contentRect: NSRect(x: 0, y: 0, width: 220, height: 44))

        let container = WidgetContainerView()
        container.onTap = { [weak self] in self?.handleWidgetTap() }
        container.translatesAutoresizingMaskIntoConstraints = false

        let hosting = NSHostingView(rootView: WidgetView().environmentObject(store))
        hosting.translatesAutoresizingMaskIntoConstraints = false
        hosting.sizingOptions = .intrinsicContentSize
        widgetHosting = hosting

        // WidgetContainerView captures all mouse events; hosting view is visual only.
        container.addSubview(hosting)
        NSLayoutConstraint.activate([
            hosting.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            hosting.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            hosting.topAnchor.constraint(equalTo: container.topAnchor),
            hosting.bottomAnchor.constraint(equalTo: container.bottomAnchor),
        ])

        panel.contentView = container

        panel.restorePosition()
        panel.orderFront(nil)

        // Initial sizing after first layout pass
        DispatchQueue.main.async { self.resizePanel() }

        // Resize whenever the task name changes. The extra async hop lets SwiftUI
        // finish its render pass and update the hosting view's intrinsic content size
        // before we query fittingSize.
        taskObservation = store.$currentTaskName
            .dropFirst()
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in DispatchQueue.main.async { self?.resizePanel() } }
    }

    private func resizePanel() {
        guard let hosting = widgetHosting else { return }
        let size = hosting.fittingSize
        let w = max(size.width, 180)
        let h = max(size.height, 36)
        // Anchor the right edge so the widget doesn't jump sideways
        let frame = panel.frame
        panel.setFrame(NSRect(x: frame.maxX - w, y: frame.origin.y, width: w, height: h),
                       display: true, animate: false)
        panel.savePosition()
    }

    // MARK: - Status Item

    private func setupStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: "checklist", accessibilityDescription: "FocusOn")
            button.image?.isTemplate = true
            button.action = #selector(statusItemClicked)
            button.target = self
        }
    }

    @objc private func statusItemClicked() {
        showActionPopover(relativeTo: statusItem.button?.bounds ?? .zero,
                         of: statusItem.button?.window ?? NSApp.keyWindow ?? NSApp.windows.first!)
    }

    // MARK: - Widget tap

    private func handleWidgetTap() {
        if store.currentTaskName == nil {
            showTaskSelection(completingPrevious: false)
        } else {
            showActionPopover(relativeTo: panel.contentView?.bounds ?? .zero, of: panel)
        }
    }

    // MARK: - Popovers

    func showActionPopover(relativeTo rect: NSRect, of window: NSWindow) {
        dismissPopover()

        let pop = NSPopover()
        pop.behavior = .transient
        pop.animates = true

        let view = ActionPopoverView(
            onCompleteTask: { [weak self, weak pop] in
                pop?.close()
                self?.store.completeCurrentTask()
                self?.showTaskSelection(completingPrevious: true)
            },
            onPauseTask: { [weak self, weak pop] in
                pop?.close()
                self?.store.pauseCurrentTask()
            },
            onChangeTask: { [weak self, weak pop] in
                pop?.close()
                self?.showTaskSelection(completingPrevious: false)
            },
            onChangeDataDirectory: { [weak self, weak pop] in
                pop?.close()
                self?.pickDataDirectory()
            },
            onQuit: {
                NSApp.terminate(nil)
            }
        )
        .environmentObject(store)

        pop.contentViewController = NSHostingController(rootView: view)
        pop.contentSize = NSSize(width: 240, height: 180)

        if let contentView = window.contentView {
            pop.show(relativeTo: rect, of: contentView, preferredEdge: .minY)
        }
        popover = pop
    }

    func showTaskSelection(completingPrevious: Bool) {
        dismissPopover()

        let pop = NSPopover()
        pop.behavior = .transient
        pop.animates = true

        let view = TaskSelectionView(
            onSelect: { [weak self, weak pop] name, project, completing in
                pop?.close()
                self?.store.startTask(name, project: project, completingPrevious: completing)
            },
            onLogPast: { [weak self, weak pop] project, task, from, to, completed in
                pop?.close()
                self?.store.logPastSession(project: project, task: task, from: from, to: to, completed: completed)
            },
            onCancel: { [weak pop] in
                pop?.close()
            },
            completingPrevious: completingPrevious
        )
        .environmentObject(store)

        pop.contentViewController = NSHostingController(rootView: view)
        pop.contentSize = NSSize(width: 280, height: 360)

        if let contentView = panel.contentView {
            pop.show(relativeTo: contentView.bounds, of: contentView, preferredEdge: .maxY)
        }
        popover = pop
    }

    private func dismissPopover() {
        popover?.close()
        popover = nil
    }

    private func pickDataDirectory() {
        let panel = NSOpenPanel()
        panel.title = "Choose FocusOn data directory"
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.canCreateDirectories = true
        panel.directoryURL = CSVLogger.dataDirectoryURL
        panel.begin { [weak self] response in
            guard response == .OK, let url = panel.url else { return }
            CSVLogger.setDataDirectory(url)
            CSVLogger.bootstrapDataDirectoryIfNeeded()
            self?.store.refreshAvailableProjects()
        }
    }
}
