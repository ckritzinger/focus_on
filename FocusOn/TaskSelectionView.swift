import SwiftUI

struct TaskSelectionView: View {
    @EnvironmentObject var store: TaskStore
    var onSelect: (String, String, Bool) -> Void   // (taskName, projectSlug, completingPrevious)
    var onLogPast: (String, String, Date, Date, Bool) -> Void  // (projectSlug, taskName, from, to, completed)
    var onCancel: () -> Void
    var completingPrevious: Bool

    @State private var selectedProject: String = "personal"
    @State private var visibleRecentTasks: [TaskStore.RecentTask] = []
    @State private var newTaskText: String = ""
    @FocusState private var fieldFocused: Bool

    // The New task row's From/To pickers double as the old "Log past
    // session" popover, folded in here instead of being a separate menu
    // item — that popover was flaky (see LogPastSessionView's removal).
    // Both default to "now", which is indistinguishable from "untouched";
    // hasEditedTimes is the actual signal for "the user wants to backdate
    // this, not start it live", set the moment either picker changes.
    @State private var from: Date = Date()
    @State private var to: Date = Date()
    @State private var pastCompleted: Bool = true
    @State private var hasEditedTimes: Bool = false

    private let formatter: RelativeDateTimeFormatter = {
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .abbreviated
        return f
    }()

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Select task")
                    .font(.headline)
                Spacer()
                Button("Cancel", action: onCancel)
                    .buttonStyle(.plain)
                    .foregroundColor(.secondary)
                    .font(.callout)
            }
            .padding(.horizontal, 12)
            .padding(.top, 12)
            .padding(.bottom, 8)

            Divider()

            HStack(spacing: 6) {
                Text("Project")
                    .font(.caption)
                    .foregroundColor(.secondary)
                Spacer()
                Picker("", selection: $selectedProject) {
                    ForEach(store.availableProjects, id: \.self) { project in
                        Text(project).tag(project)
                    }
                }
                .labelsHidden()
                .pickerStyle(.menu)
                .frame(maxWidth: 160)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .onChange(of: selectedProject) { _ in
                refreshVisibleRecentTasks()
            }

            Divider()

            if visibleRecentTasks.isEmpty {
                Text("No recent tasks")
                    .font(.callout)
                    .foregroundColor(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.vertical, 16)
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(visibleRecentTasks) { task in
                            Button {
                                onSelect(task.name, selectedProject, completingPrevious)
                            } label: {
                                HStack {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(task.name)
                                            .font(.callout)
                                            .foregroundColor(.primary)
                                            .lineLimit(1)
                                        Text(formatter.localizedString(for: task.startedAt, relativeTo: Date()))
                                            .font(.caption2)
                                            .foregroundColor(.secondary)
                                    }
                                    Spacer()
                                }
                                .padding(.horizontal, 12)
                                .padding(.vertical, 7)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                .frame(maxHeight: 180)

                Divider()
            }

            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) {
                    TextField("New task…", text: $newTaskText)
                        .textFieldStyle(.plain)
                        .font(.callout)
                        .focused($fieldFocused)
                        .onSubmit { commitNewTask() }

                    Button(hasEditedTimes ? "Log" : "Start", action: commitNewTask)
                        .buttonStyle(.borderedProminent)
                        .controlSize(.small)
                        .disabled(newTaskText.trimmingCharacters(in: .whitespaces).isEmpty || (hasEditedTimes && to <= from))
                }

                HStack(spacing: 6) {
                    Text("From")
                        .font(.caption2)
                        .foregroundColor(.secondary)
                        .frame(width: 28, alignment: .leading)
                    DatePicker("", selection: $from)
                        .labelsHidden()
                        .font(.caption2)
                        .onChange(of: from) { _ in hasEditedTimes = true }
                    Spacer()
                }
                HStack(spacing: 6) {
                    Text("To")
                        .font(.caption2)
                        .foregroundColor(.secondary)
                        .frame(width: 28, alignment: .leading)
                    DatePicker("", selection: $to)
                        .labelsHidden()
                        .font(.caption2)
                        .onChange(of: to) { _ in hasEditedTimes = true }
                    Spacer()
                    if hasEditedTimes {
                        Toggle("Done", isOn: $pastCompleted)
                            .toggleStyle(.checkbox)
                            .font(.caption2)
                    }
                }

                if hasEditedTimes && to <= from {
                    Text("To must be after From")
                        .font(.caption2)
                        .foregroundColor(.red)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
        }
        .frame(width: 280)
        .onAppear {
            selectedProject = store.currentProjectSlug
            refreshVisibleRecentTasks()
            // Deferred a tick: the project Picker's onChange-driven state
            // mutation above can still be settling into a re-render at the
            // moment onAppear fires, and requesting focus mid-churn is a
            // known way for @FocusState to silently lose the request in an
            // NSPopover. Letting that settle first is what actually gets the
            // field focused reliably.
            DispatchQueue.main.async {
                fieldFocused = true
            }
        }
    }

    private func refreshVisibleRecentTasks() {
        visibleRecentTasks = store.recentTasks(forProject: selectedProject)
    }

    private func commitNewTask() {
        let name = newTaskText.trimmingCharacters(in: .whitespaces)
        guard !name.isEmpty else { return }
        if hasEditedTimes {
            guard to > from else { return }
            onLogPast(selectedProject, name, from, to, pastCompleted)
        } else {
            onSelect(name, selectedProject, completingPrevious)
        }
    }
}
