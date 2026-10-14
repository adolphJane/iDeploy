import SwiftUI

private struct DatabaseFormRequest: Identifiable {
    let id = UUID()
    let connection: J?
}

struct DatabasesPage: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedID: String?
    @State private var schema = ""
    @State private var schemas: [J] = []
    @State private var tables: [J] = []
    @State private var selectedTable = ""
    @State private var columns: [J] = []
    @State private var indexes: [J] = []
    @State private var page = J(nil)
    @State private var pageNumber = 1
    @State private var sortColumn = ""
    @State private var sortDesc = false
    @State private var tab = 0
    @State private var formRequest: DatabaseFormRequest?
    @State private var deleteTarget: J?
    @State private var browserMessage = ""
    @State private var connectionMessage = ""
    @State private var testingConnection = false
    @State private var loadingSchemas = false
    @State private var loadingTables = false

    private var selected: J? { model.databases.first { $0.id == selectedID } }

    var body: some View {
        HStack(spacing: 0) {
            VStack(spacing: 0) {
                HStack {
                    WorkspaceListHeader(title: "数据库", count: model.databases.count)
                    Spacer()
                    Button { formRequest = DatabaseFormRequest(connection: nil) } label: { Image(systemName: "plus") }
                        .buttonStyle(DeploySecondaryButtonStyle())
                        .help("添加数据库")
                }
                .padding(14)
                .background(DeployStyle.overviewHeader)
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                ScrollView(.vertical, showsIndicators: false) {
                    LazyVStack(spacing: 5) {
                        ForEach(model.databases, id: \.id) { connection in
                            let isSelected = selectedID == connection.id
                            Button {
                                selectedID = connection.id
                            } label: {
                                HStack(spacing: 11) {
                                    Image(systemName: "externaldrive")
                                        .font(.system(size: 15, weight: .medium))
                                        .foregroundStyle(DeployStyle.green)
                                        .frame(width: 30, height: 30)
                                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 8))
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(connection["name"].string)
                                            .font(.system(size: 12, weight: .semibold))
                                            .foregroundStyle(DeployStyle.ink)
                                            .lineLimit(1)
                                        Text("\(connection["host"].string):\(connection["port"].string)")
                                            .font(.system(size: 10, design: .monospaced))
                                            .foregroundStyle(DeployStyle.muted)
                                            .lineLimit(1)
                                    }
                                    Spacer(minLength: 0)
                                    if isSelected {
                                        Capsule().fill(DeployStyle.greenBright).frame(width: 3, height: 20)
                                    }
                                }
                                .padding(.horizontal, 12)
                                .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
                                .background(isSelected ? DeployStyle.greenSoft : Color.clear,
                                            in: RoundedRectangle(cornerRadius: 10))
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .accessibilityAddTraits(isSelected ? .isSelected : [])
                        }
                    }
                    .padding(10)
                }
                .background(DeployStyle.canvas)
            }
            .frame(minWidth: 240, idealWidth: 280, maxWidth: 330)
            .background(DeployStyle.canvas)
            Rectangle().fill(DeployStyle.line).frame(width: 1)
            if let connection = selected {
                PageContainer {
                    HStack {
                        SectionHeading(connection["name"].string,
                                       connection["databaseName"].string.isEmpty
                                           ? connection["driver"].string.uppercased()
                                           : "\(connection["driver"].string.uppercased()) · \(connection["databaseName"].string)")
                        Spacer()
                    }
                    HStack(spacing: 10) {
                        Button(testingConnection ? "正在测试…" : "测试连接") { testConnection(connection.id) }
                            .buttonStyle(DeploySecondaryButtonStyle())
                            .disabled(testingConnection)
                        Button("编辑连接") { formRequest = DatabaseFormRequest(connection: connection) }
                            .buttonStyle(DeploySecondaryButtonStyle())
                        Spacer()
                        Button("移除", role: .destructive) { deleteTarget = connection }
                            .buttonStyle(DeploySecondaryButtonStyle())
                    }
                    HStack(spacing: 12) {
                        InfoTile(title: "驱动", value: connection["driver"].string.uppercased(), symbol: "externaldrive")
                        InfoTile(title: "数据库", value: connection["databaseName"].string.isEmpty ? "未指定" : connection["databaseName"].string, symbol: "cylinder.split.1x2")
                        InfoTile(title: "数据表", value: schema.isEmpty ? "—" : "\(tables.count)", symbol: "tablecells")
                    }
                    VStack(alignment: .leading, spacing: 14) {
                        SectionHeading("连接信息", "远程数据库与只读访问账号")
                        HStack(spacing: 24) {
                            databaseDetail("主机地址", "\(connection["host"].string):\(connection["port"].string)")
                            databaseDetail("登录用户", connection["username"].string)
                            databaseDetail("TLS", connection["sslMode"].string == "disable" ? "未启用" : "已启用")
                        }
                        if !connection["lastError"].string.isEmpty {
                            Text(connection["lastError"].string)
                                .font(.system(size: 11))
                                .foregroundStyle(.orange)
                                .textSelection(.enabled)
                        }
                    }
                    .padding(18)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)
                    databaseBrowser(connection)
                }
                .task(id: connection.id) { loadSchemas(connection.id) }
                .overlay(alignment: .bottomTrailing) {
                    if !connectionMessage.isEmpty {
                        HStack(alignment: .top, spacing: 10) {
                            Image(systemName: testingConnection ? "arrow.triangle.2.circlepath" :
                                  connectionMessage == "数据库连接成功。" ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                                .foregroundStyle(connectionMessage == "数据库连接成功。" ? DeployStyle.green : .orange)
                            Text(connectionMessage)
                                .font(.system(size: 12))
                                .foregroundStyle(DeployStyle.ink)
                                .lineLimit(3)
                                .textSelection(.enabled)
                                .help(connectionMessage)
                            Button { connectionMessage = "" } label: {
                                Image(systemName: "xmark")
                                    .font(.system(size: 10, weight: .semibold))
                            }
                            .buttonStyle(.plain)
                            .foregroundStyle(DeployStyle.muted)
                            .help("关闭提示")
                        }
                        .padding(14)
                        .frame(maxWidth: 410, alignment: .leading)
                        .deployCard(fill: DeployStyle.overviewPanel)
                        .padding(24)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                        .accessibilityElement(children: .contain)
                    }
                }
                .animation(.easeOut(duration: 0.2), value: connectionMessage.isEmpty)
            } else {
                VStack(spacing: 12) {
                    Image(systemName: "externaldrive")
                        .font(.system(size: 28, weight: .light))
                        .foregroundStyle(DeployStyle.green)
                        .frame(width: 68, height: 68)
                        .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 19))
                    Text("选择数据库连接")
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("查看连接信息、数据表和只读数据。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(DeployStyle.canvas)
            }
        }
        .environment(\.colorScheme, .light)
        .sheet(item: $formRequest) { request in
            DatabaseFormView(connection: request.connection)
                .environmentObject(model)
                .id(request.id)
        }
        .onChange(of: selectedID) { _ in
            schema = ""
            schemas = []
            tables = []
            selectedTable = ""
            columns = []
            indexes = []
            page = J(nil)
            browserMessage = ""
            connectionMessage = ""
            loadingSchemas = false
            loadingTables = false
        }
        .confirmationDialog("移除数据库连接？", isPresented: Binding(
            get: { deleteTarget != nil },
            set: { if !$0 { deleteTarget = nil } }
        )) {
            Button("移除", role: .destructive) {
                guard let target = deleteTarget else { return }
                Task {
                    await model.perform("DeleteDatabaseConnection", [target.id])
                    selectedID = nil
                    deleteTarget = nil
                }
            }
        }
    }

    private func databaseDetail(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(.system(size: 11))
                .foregroundStyle(DeployStyle.muted)
            Text(value.isEmpty ? "—" : value)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(DeployStyle.ink)
                .lineLimit(1)
                .textSelection(.enabled)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func databaseBrowser(_ connection: J) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                SectionHeading("浏览数据", "选择 Schema 和数据表，以只读方式查看内容")
                Spacer()
                Label("只读", systemImage: "eye")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(DeployStyle.green)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(DeployStyle.greenSoft, in: Capsule())
            }
            if !browserMessage.isEmpty {
                Text(browserMessage)
                    .font(.system(size: 11))
                    .foregroundStyle(.orange)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(11)
                    .background(Color.orange.opacity(0.08), in: RoundedRectangle(cornerRadius: 9))
            }
            HStack(alignment: .top, spacing: 0) {
                VStack(alignment: .leading, spacing: 12) {
                    HStack {
                        Text("数据表")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(DeployStyle.ink)
                        Spacer()
                        Text("\(tables.count)")
                            .font(.system(size: 11))
                            .foregroundStyle(DeployStyle.muted)
                    }
                    Picker("Schema", selection: $schema) {
                        if schemas.isEmpty { Text(loadingSchemas ? "正在读取…" : "暂无 Schema").tag("") }
                        ForEach(schemas, id: \.selfName) { item in
                            Text(item["name"].string).tag(item["name"].string)
                        }
                    }
                    .labelsHidden()
                    .frame(maxWidth: .infinity)
                    .onChange(of: schema) { _ in loadTables(connection.id) }
                    Rectangle().fill(DeployStyle.line).frame(height: 1)
                    ScrollView(.vertical, showsIndicators: false) {
                        LazyVStack(spacing: 5) {
                            ForEach(tables, id: \.selfName) { table in
                                let isSelected = selectedTable == table["name"].string
                                Button {
                                    selectedTable = table["name"].string
                                    tab = 0
                                } label: {
                                    HStack(spacing: 9) {
                                        Image(systemName: table["type"].string == "VIEW" ? "eye" : "tablecells")
                                            .foregroundStyle(DeployStyle.green)
                                        Text(table["name"].string)
                                            .font(.system(size: 11, weight: isSelected ? .semibold : .regular))
                                            .foregroundStyle(DeployStyle.ink)
                                            .lineLimit(1)
                                        Spacer(minLength: 0)
                                    }
                                    .padding(.horizontal, 10)
                                    .frame(maxWidth: .infinity, minHeight: 34, alignment: .leading)
                                    .background(isSelected ? DeployStyle.greenSoft : Color.clear,
                                                in: RoundedRectangle(cornerRadius: 8))
                                    .contentShape(Rectangle())
                                }
                                .buttonStyle(.plain)
                                .accessibilityAddTraits(isSelected ? .isSelected : [])
                            }
                        }
                    }
                    .frame(height: 350)
                    if tables.isEmpty {
                        Text(loadingTables ? "正在读取数据表…" : schema.isEmpty ? "选择 Schema 后显示数据表" : "此 Schema 暂无数据表")
                            .font(.system(size: 11))
                            .foregroundStyle(DeployStyle.muted)
                    }
                }
                .padding(14)
                .frame(width: 235, alignment: .leading)
                Rectangle().fill(DeployStyle.line).frame(width: 1)
                tablePreview(connection.id)
                    .frame(maxWidth: .infinity, minHeight: 440, alignment: .topLeading)
            }
            .background(DeployStyle.canvas, in: RoundedRectangle(cornerRadius: 11))
            .overlay {
                RoundedRectangle(cornerRadius: 11)
                    .strokeBorder(DeployStyle.line, lineWidth: 1)
            }
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .deployCard(fill: DeployStyle.overviewPanel)
    }

    @ViewBuilder
    private func tablePreview(_ connectionID: String) -> some View {
        if selectedTable.isEmpty {
            VStack(spacing: 10) {
                Image(systemName: "tablecells")
                    .font(.system(size: 24, weight: .light))
                    .foregroundStyle(DeployStyle.green)
                    .frame(width: 54, height: 54)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 15))
                Text("选择一张表")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(DeployStyle.ink)
                Text("查看数据、列定义与索引")
                    .font(.system(size: 11))
                    .foregroundStyle(DeployStyle.muted)
            }
            .frame(maxWidth: .infinity, minHeight: 440)
        } else {
            VStack(spacing: 0) {
                HStack(spacing: 12) {
                    Text(selectedTable)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(DeployStyle.ink)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    Picker("内容", selection: $tab) {
                        Text("数据").tag(0)
                        Text("列").tag(1)
                        Text("索引").tag(2)
                    }
                    .pickerStyle(.segmented)
                    .frame(width: 220)
                }
                .padding(14)
                Rectangle().fill(DeployStyle.line).frame(height: 1)
                Group {
                    switch tab {
                    case 1: columnsView
                    case 2: indexesView
                    default: rowsView
                    }
                }
                .frame(maxWidth: .infinity, minHeight: 380)
            }
            .task(id: selectedTable) { loadTableDetail(connectionID) }
        }
    }

    private var columnsView: some View {
        ScrollView(.vertical) {
            LazyVStack(spacing: 0) {
                ForEach(columns, id: \.selfName) { column in
                    HStack(spacing: 10) {
                        Image(systemName: column["primaryKey"].bool ? "key.fill" : "circle.grid.2x2")
                            .foregroundStyle(column["primaryKey"].bool ? .orange : DeployStyle.green)
                            .frame(width: 18)
                        Text(column["name"].string)
                            .font(.system(size: 11, weight: .semibold, design: .monospaced))
                            .foregroundStyle(DeployStyle.ink)
                        Spacer()
                        Text(column["dataType"].string)
                            .font(.system(size: 11, design: .monospaced))
                            .foregroundStyle(DeployStyle.muted)
                        if !column["nullable"].bool {
                            Text("NOT NULL")
                                .font(.system(size: 9, weight: .semibold))
                                .foregroundStyle(DeployStyle.green)
                        }
                    }
                    .padding(.horizontal, 14)
                    .frame(minHeight: 42)
                    Rectangle().fill(DeployStyle.line).frame(height: 1)
                }
            }
        }
        .background(DeployStyle.overviewPanel)
    }

    private var indexesView: some View {
        ScrollView(.vertical) {
            LazyVStack(spacing: 0) {
                ForEach(indexes, id: \.selfName) { index in
                    HStack(spacing: 10) {
                        Image(systemName: "list.bullet.indent")
                            .foregroundStyle(DeployStyle.green)
                        Text(index["name"].string)
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(DeployStyle.ink)
                        Spacer()
                        Text(index["columns"].array.map(\.string).joined(separator: ", "))
                            .font(.system(size: 11, design: .monospaced))
                            .foregroundStyle(DeployStyle.muted)
                        if index["unique"].bool {
                            Text("UNIQUE")
                                .font(.system(size: 9, weight: .semibold))
                                .foregroundStyle(DeployStyle.green)
                        }
                    }
                    .padding(.horizontal, 14)
                    .frame(minHeight: 42)
                    Rectangle().fill(DeployStyle.line).frame(height: 1)
                }
            }
        }
        .background(DeployStyle.overviewPanel)
    }

    private var rowsView: some View {
        VStack(spacing: 0) {
            if page["rows"].array.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "tablecells")
                        .font(.system(size: 22, weight: .light))
                        .foregroundStyle(DeployStyle.green)
                    Text("当前页没有可显示的数据")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(DeployStyle.ink)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView([.horizontal, .vertical]) {
                    Grid(alignment: .leading, horizontalSpacing: 18, verticalSpacing: 8) {
                        GridRow {
                            ForEach(page["columns"].array, id: \.selfName) { column in
                                Button {
                                    let name = column["name"].string
                                    if sortColumn == name { sortDesc.toggle() }
                                    else { sortColumn = name; sortDesc = false }
                                    pageNumber = 1
                                    if let selected { loadRows(selected.id) }
                                } label: {
                                    HStack {
                                        Text(column["name"].string).fontWeight(.semibold)
                                        if sortColumn == column["name"].string {
                                            Image(systemName: sortDesc ? "arrow.down" : "arrow.up")
                                        }
                                    }
                                    .foregroundStyle(DeployStyle.ink)
                                    .frame(minWidth: 130, alignment: .leading)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        Divider()
                        ForEach(Array(page["rows"].array.enumerated()), id: \.offset) { _, row in
                            GridRow {
                                ForEach(page["columns"].array, id: \.selfName) { column in
                                    Text(row[column["name"].string].string)
                                        .font(.system(.caption, design: .monospaced))
                                        .foregroundStyle(DeployStyle.ink)
                                        .lineLimit(3)
                                        .textSelection(.enabled)
                                        .frame(minWidth: 130, alignment: .leading)
                                }
                            }
                        }
                    }
                    .padding(14)
                }
            }
            Divider()
            HStack {
                Button("上一页") {
                    pageNumber = max(1, pageNumber - 1)
                    if let selected { loadRows(selected.id) }
                }
                .disabled(pageNumber <= 1)
                Text("第 \(pageNumber) 页").foregroundStyle(DeployStyle.muted)
                Button("下一页") {
                    pageNumber += 1
                    if let selected { loadRows(selected.id) }
                }
                .disabled(!page["hasNext"].bool)
                Spacer()
                Text("只读 · 每页 50 行").font(.caption).foregroundStyle(DeployStyle.muted)
            }
            .padding(10)
            .background(DeployStyle.overviewPanel)
        }
        .background(DeployStyle.overviewPanel)
    }

    private func loadSchemas(_ id: String) {
        Task {
            loadingSchemas = true
            browserMessage = ""
            do {
                let loaded = try await NativeBridge.call("ListDatabaseSchemas", [id]).array
                guard selectedID == id else { return }
                loadingSchemas = false
                schemas = loaded
                schema = loaded.first?["name"].string ?? ""
                if schema.isEmpty { tables = [] }
            } catch {
                guard selectedID == id else { return }
                loadingSchemas = false
                schemas = []
                tables = []
                browserMessage = "无法读取 Schema：\(error.localizedDescription)"
            }
        }
    }
    private func loadTables(_ id: String) {
        guard !schema.isEmpty else { return }
        let requestedSchema = schema
        Task {
            loadingTables = true
            do {
                let loaded = try await NativeBridge.call("ListDatabaseTables", [id, requestedSchema]).array
                guard selectedID == id, schema == requestedSchema else { return }
                loadingTables = false
                tables = loaded
                selectedTable = ""
                browserMessage = ""
            } catch {
                guard selectedID == id, schema == requestedSchema else { return }
                loadingTables = false
                tables = []
                selectedTable = ""
                browserMessage = "无法读取数据表：\(error.localizedDescription)"
            }
        }
    }
    private func testConnection(_ id: String) {
        Task {
            testingConnection = true
            connectionMessage = "正在测试数据库连接…"
            defer { testingConnection = false }
            do {
                _ = try await NativeBridge.call("TestSavedDatabaseConnection", [id])
                guard selectedID == id else { return }
                connectionMessage = "数据库连接成功。"
                await model.refresh()
                loadSchemas(id)
            } catch {
                guard selectedID == id else { return }
                connectionMessage = "连接失败：\(error.localizedDescription)"
                await model.refresh()
            }
        }
    }
    private func loadTableDetail(_ id: String) {
        guard !selectedTable.isEmpty else { return }
        Task {
            columns = (await model.perform("GetDatabaseTableColumns", [id, schema, selectedTable], refreshAfter: false) ?? J(nil)).array
            indexes = (await model.perform("GetDatabaseTableIndexes", [id, schema, selectedTable], refreshAfter: false) ?? J(nil)).array
            pageNumber = 1
            sortColumn = ""
            sortDesc = false
            loadRows(id)
        }
    }
    private func loadRows(_ id: String) {
        Task {
            let query: [String: Any] = ["page": pageNumber, "pageSize": 50, "sortColumn": sortColumn, "sortDesc": sortDesc]
            page = await model.perform("GetDatabaseTableRows", [id, schema, selectedTable, query], refreshAfter: false) ?? J(nil)
        }
    }
}

private extension J {
    var selfName: String { self["name"].string }
}

struct DatabaseFormView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let connection: J?
    @State private var name: String
    @State private var driver: String
    @State private var host: String
    @State private var port: String
    @State private var databaseName: String
    @State private var username: String
    @State private var password = ""
    @State private var sslMode: String
    @State private var testMessage = ""
    @State private var testSucceeded = false
    @State private var isTesting = false
    @State private var isSaving = false

    init(connection: J?) {
        self.connection = connection
        _name = State(initialValue: connection?["name"].string ?? "")
        _driver = State(initialValue: connection?["driver"].string ?? "postgres")
        _host = State(initialValue: connection?["host"].string ?? "")
        _port = State(initialValue: connection?["port"].string ?? "5432")
        _databaseName = State(initialValue: connection?["databaseName"].string ?? "")
        _username = State(initialValue: connection?["username"].string ?? "")
        _sslMode = State(initialValue: connection?["sslMode"].string ?? "require")
    }

    private var input: [String: Any] {
        ["name": name.trimmingCharacters(in: .whitespacesAndNewlines),
         "driver": driver,
         "host": host.trimmingCharacters(in: .whitespacesAndNewlines),
         "port": Int(port) ?? 0,
         "databaseName": databaseName.trimmingCharacters(in: .whitespacesAndNewlines),
         "username": username.trimmingCharacters(in: .whitespacesAndNewlines),
         "password": password,
         "sslMode": sslMode]
    }

    private var canSave: Bool {
        !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
        !host.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
        (driver == "mysql" || !databaseName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) &&
        !username.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
        (Int(port).map { (1...65535).contains($0) } ?? false)
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 14) {
                Image(systemName: "externaldrive")
                    .font(.system(size: 20, weight: .medium))
                    .foregroundStyle(DeployStyle.green)
                    .frame(width: 48, height: 48)
                    .background(DeployStyle.greenSoft, in: RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 5) {
                    Text(connection == nil ? "添加数据库连接" : "编辑数据库连接")
                        .font(.system(size: 21, weight: .bold))
                        .foregroundStyle(DeployStyle.ink)
                    Text("配置 PostgreSQL 或 MySQL 的只读数据访问。")
                        .font(.system(size: 12))
                        .foregroundStyle(DeployStyle.muted)
                }
                Spacer()
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(DeployStyle.overviewHeader)

            ScrollView(.vertical, showsIndicators: true) {
                VStack(alignment: .leading, spacing: 14) {
                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("连接信息", "设置名称、数据库类型和远程地址")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("名称").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("例如 生产 MySQL", text: $name)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("驱动").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                Picker("驱动", selection: $driver) {
                                    Text("PostgreSQL").tag("postgres")
                                    Text("MySQL / MariaDB").tag("mysql")
                                }
                                .labelsHidden()
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("主机地址").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("域名或 IP 地址", text: $host)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("端口").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField(driver == "mysql" ? "3306" : "5432", text: $port)
                                    .textFieldStyle(.roundedBorder)
                            }
                            .frame(width: 120)
                        }
                        VStack(alignment: .leading, spacing: 7) {
                            Text(driver == "mysql" ? "数据库名（可选）" : "数据库名")
                                .font(.system(size: 12, weight: .semibold))
                                .foregroundStyle(DeployStyle.ink)
                            TextField("要访问的数据库", text: $databaseName)
                                .textFieldStyle(.roundedBorder)
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)

                    VStack(alignment: .leading, spacing: 15) {
                        SectionHeading("只读账号", "凭据保存在本机钥匙串中")
                        HStack(alignment: .top, spacing: 16) {
                            VStack(alignment: .leading, spacing: 7) {
                                Text("用户名").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                TextField("数据库登录用户", text: $username)
                                    .textFieldStyle(.roundedBorder)
                            }
                            VStack(alignment: .leading, spacing: 7) {
                                Text("密码").font(.system(size: 12, weight: .semibold)).foregroundStyle(DeployStyle.ink)
                                SecureField("输入数据库密码", text: $password)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                        HStack(spacing: 12) {
                            Text("TLS 加密")
                                .font(.system(size: 12, weight: .semibold))
                                .foregroundStyle(DeployStyle.ink)
                            Spacer()
                            Picker("TLS 加密", selection: $sslMode) {
                                Text("要求加密").tag("require")
                                Text("关闭").tag("disable")
                            }
                            .labelsHidden()
                            .frame(width: 150)
                        }
                        if connection != nil {
                            Label("密码留空时保留钥匙串中已保存的凭据。", systemImage: "key.horizontal")
                                .font(.system(size: 11))
                                .foregroundStyle(DeployStyle.muted)
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .deployCard(fill: DeployStyle.overviewPanel)
                }
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
            }
            .background(DeployStyle.canvas)

            Rectangle().fill(DeployStyle.line).frame(height: 1)
            if !testMessage.isEmpty {
                Label(testMessage, systemImage: isTesting ? "arrow.triangle.2.circlepath" : testSucceeded ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                    .font(.system(size: 11))
                    .foregroundStyle(testSucceeded ? DeployStyle.green : DeployStyle.ink)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 24)
                    .padding(.vertical, 9)
                    .background(testSucceeded ? DeployStyle.greenSoft : DeployStyle.overviewHeader)
                    .accessibilityAddTraits(.updatesFrequently)
            }
            HStack {
                Button {
                    guard canSave else {
                        testSucceeded = false
                        testMessage = driver == "mysql"
                            ? "请填写名称、地址、端口和用户名后再测试。"
                            : "请填写名称、地址、端口、数据库名和用户名后再测试。"
                        return
                    }
                    Task {
                        isTesting = true
                        testSucceeded = false
                        testMessage = "正在测试数据库连接…"
                        defer { isTesting = false }
                        do {
                            _ = try await NativeBridge.call("TestDatabaseConnection", [input])
                            testSucceeded = true
                            testMessage = "数据库连接成功。"
                        } catch {
                            testMessage = "连接失败：\(error.localizedDescription)"
                        }
                    }
                } label: {
                    if isTesting { ProgressView().controlSize(.small) }
                    Text(isTesting ? "正在测试…" : "测试连接")
                }
                .buttonStyle(DeploySecondaryButtonStyle())
                .disabled(isTesting || isSaving)
                Spacer()
                Button("取消") { dismiss() }
                    .buttonStyle(DeploySecondaryButtonStyle())
                    .keyboardShortcut(.cancelAction)
                Button("保存") {
                    Task {
                        isSaving = true
                        defer { isSaving = false }
                        do {
                            _ = try await NativeBridge.call(connection == nil ? "CreateDatabaseConnection" : "UpdateDatabaseConnection",
                                                            connection == nil ? [input] : [connection!.id, input])
                            await model.refresh()
                            dismiss()
                        } catch {
                            testSucceeded = false
                            testMessage = "保存失败：\(error.localizedDescription)"
                        }
                    }
                }
                .buttonStyle(DeployPrimaryButtonStyle())
                .keyboardShortcut(.defaultAction)
                .disabled(!canSave || isTesting || isSaving)
                .opacity(canSave && !isTesting && !isSaving ? 1 : 0.5)
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
            .background(DeployStyle.overviewPanel)
        }
        .frame(width: 680, height: 640)
        .environment(\.colorScheme, .light)
        .onChange(of: driver) { newDriver in
            if newDriver == "mysql" && port == "5432" { port = "3306" }
            if newDriver == "postgres" && port == "3306" { port = "5432" }
        }
    }
}
