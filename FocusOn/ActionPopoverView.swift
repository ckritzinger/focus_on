import SwiftUI
import ServiceManagement

struct ActionPopoverView: View {
    @EnvironmentObject var store: TaskStore
    var onCompleteTask: () -> Void
    var onPauseTask: () -> Void
    var onChangeTask: () -> Void
    var onChangeDataDirectory: () -> Void
    var onQuit: () -> Void

    @State private var launchAtLogin: Bool = (SMAppService.mainApp.status == .enabled)
    @State private var dataDirPath: String = CSVLogger.dataDirectoryDisplayPath

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if store.currentTaskName != nil {
                Button(action: onCompleteTask) {
                    Label("Complete task", systemImage: "checkmark")
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .buttonStyle(.plain)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)

                Button(action: onPauseTask) {
                    Label("Pause task", systemImage: "pause.circle")
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .buttonStyle(.plain)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)

                Button(action: onChangeTask) {
                    Label("Change task", systemImage: "arrow.uturn.left")
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .buttonStyle(.plain)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)

                Divider().padding(.vertical, 4)
            }

            Toggle(isOn: $launchAtLogin) {
                Text("Launch at login")
                    .font(.callout)
            }
            .toggleStyle(.checkbox)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .onChange(of: launchAtLogin) { newValue in
                do {
                    if newValue {
                        try SMAppService.mainApp.register()
                    } else {
                        try SMAppService.mainApp.unregister()
                    }
                } catch {}
            }

            Divider().padding(.vertical, 4)

            HStack(spacing: 6) {
                Text(dataDirPath)
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 4)
                Button("Change…") {
                    onChangeDataDirectory()
                    dataDirPath = CSVLogger.dataDirectoryDisplayPath
                }
                .font(.caption)
                .buttonStyle(.plain)
                .foregroundColor(.accentColor)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)

            Divider().padding(.vertical, 4)

            Button(action: onQuit) {
                Text("Quit")
                    .font(.callout)
                    .foregroundColor(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
        }
        .padding(.vertical, 8)
        .frame(width: 260)
    }
}
