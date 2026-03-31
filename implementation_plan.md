# USB双向文件夹同步工具 — 实现计划 v7

## 概述

通过 U 盘中转，保持两台 Windows 电脑的工作文件夹双向同步。

程序与数据库都放在 U 盘上：

- `USBSync.exe`
- `USBSync.db`

用户在当前 U 盘上启动程序，程序先加载这台电脑上次保存的启动配置并带出工作文件夹；用户确认后点击“同步”，主窗口显示详细进度表，完成后给出结果摘要、冲突清单与失败原因。

显示的机器名称可自定义，但不作为同步身份。  
本地机器会额外保存一份启动配置，用于程序启动后直接带出这台电脑对应的工作文件夹；本机文件默认统一放在 `C:\.usbsync\` 下，数据库备份也默认放在这里；工作文件夹内只保留扫描缓存与日志。

兼容目标调整为：

- Win10 64 位
- Win11 64 位
- Win10 以上内核的 WinPE 64 位环境

不再兼容 Win7。

---

## 目标边界

- 同步普通文件与目录，包含空目录
- v1 主要面向两台长期使用的 Windows 电脑；可以保留历史机器记录，但只有标记为 active 的机器参与同步判断、删除清理与验收
- v1 不做智能重命名识别；用户的重命名按“删除旧路径 + 新建新路径”处理
- 不同步符号链接、junction、Alternate Data Streams、系统保留文件；v1 明确跳过 `desktop.ini`、`Thumbs.db`、`$RECYCLE.BIN`、`System Volume Information` 等对象，遇到这些对象时记录日志并提示跳过
- 工作文件夹下保留目录 `.usbsync-local\`，用于扫描缓存与日志；该目录永远不参与同步
- 不依赖托盘、自启动、后台常驻、插盘自动触发；v1 只支持“打开程序 → 点击同步”的手动流程
- 一台电脑只绑定一个工作文件夹；更换工作文件夹时，视为整套同步内容切换到新目录，并向另一台电脑传播“需要更换工作目录”的提示
- 数据安全优先于“看起来自动化”：遇到无法可靠判断的情况，保留文件并提示，不静默覆盖

---

## 技术选型

| 项目 | 选择 | 理由 |
|------|------|------|
| Go 工具链 | Go 1.22.x | 不再受 Win7 约束，优先使用较新的稳定工具链 |
| U 盘存储 | SQLite 单文件 | 增量更新、事务安全、单文件 |
| SQLite 驱动 | `modernc.org/sqlite` | 纯 Go 实现，无 CGo，便于发布 |
| SQLite 写入模式 | rollback journal | 允许写入期间短暂出现辅助文件；同步完成后 U 盘最终只保留 `USBSync.db` |
| 主窗口 UI | `lxn/walk` | 原生 Windows 窗口与表格控件，适合手动同步与详细进度显示 |
| 运行方式 | U 盘便携单窗口 | 适配 Win10/Win11/WinPE，不依赖托盘、自启动和用户目录 |
| 本机启动配置 | 默认 `C:\.usbsync\machine.json` | 保存这台电脑的工作文件夹、备份目录和绑定信息，程序启动时直接加载 |
| 日志 | U 盘内 SQLite + 本地日志文件 | 同时满足跨机器追踪和本机排错 |
| 本机灾备 | 默认 `C:\.usbsync\backup\USBSync-latest.db` / `C:\.usbsync\backup\USBSync-prev.db`，可更改目录 | U 盘损坏时可线下覆盖恢复 |

---

## 核心设计：单文件 + 可追溯同步状态

### 为什么选 SQLite？

| 方案 | 增量更新 | 事务安全 | 单文件 | 评价 |
|------|---------|---------|--------|------|
| **SQLite** | ✅ 只更新变更数据 | ✅ ACID 事务 | ✅ | **最佳** |
| ZIP 压缩包 | ❌ 基本要全量重写 | ❌ | ✅ | 大文件夹很慢 |
| bbolt | ✅ | ✅ | ✅ | 大 BLOB 回收与追踪麻烦 |
| tar 增量包 | 部分支持 | ❌ | ✅ | 复杂且不可靠 |

### 同步状态模型

U 盘数据库不只保存“当前文件长什么样”，还要保存“发生过什么变化”。v1 采用“当前状态 + 变更日志 + 删除墓碑”的结构，避免删除丢失、顺序错乱、冲突误判。

```sql
-- U盘身份与版本
CREATE TABLE device_meta (
    device_id            TEXT PRIMARY KEY,
    schema_version       INTEGER NOT NULL,
    created_at           TEXT NOT NULL,
    active_machine_limit INTEGER NOT NULL DEFAULT 2,
    workspace_generation INTEGER NOT NULL DEFAULT 1
);

-- 机器注册表；machine_id 稳定，display_name 可改
CREATE TABLE machine_registry (
    machine_id      TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active',   -- active / retired
    first_seen_at   TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    last_work_root  TEXT
);

-- 文件内容，按内容哈希寻址；大文件分块
CREATE TABLE blobs (
    blob_id    TEXT NOT NULL,
    chunk_idx  INTEGER NOT NULL,
    content    BLOB NOT NULL,
    PRIMARY KEY (blob_id, chunk_idx)
);

-- 路径的当前状态；目录与删除墓碑也在这里表示
CREATE TABLE entries (
    path_key         TEXT PRIMARY KEY,
    display_path     TEXT NOT NULL,
    kind             TEXT NOT NULL,        -- file / dir
    size             INTEGER,
    mtime_ns         INTEGER,
    content_md5      TEXT,
    blob_id          TEXT,
    chunks           INTEGER DEFAULT 0,
    deleted          INTEGER NOT NULL DEFAULT 0,
    last_revision    INTEGER NOT NULL,
    last_machine_id  TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

-- 追加式变更日志，revision 是唯一的先后依据
CREATE TABLE change_log (
    revision      INTEGER PRIMARY KEY AUTOINCREMENT,
    machine_id    TEXT NOT NULL,
    op            TEXT NOT NULL,        -- add / modify / delete / mkdir / conflict_copy / workspace_reset
    path_key      TEXT NOT NULL,
    display_path  TEXT NOT NULL,
    kind          TEXT NOT NULL,
    base_revision INTEGER NOT NULL,
    size          INTEGER,
    mtime_ns      INTEGER,
    content_md5   TEXT,
    blob_id       TEXT,
    created_at    TEXT NOT NULL
);

-- 各机器已消费到哪一版
CREATE TABLE machine_state (
    machine_id          TEXT PRIMARY KEY,
    last_seen_revision  INTEGER NOT NULL DEFAULT 0,
    last_sync_at        TEXT,
    last_backup_at      TEXT,
    last_workspace_generation INTEGER NOT NULL DEFAULT 1
);

-- 同步会话，用于失败恢复与安全重放
CREATE TABLE sync_sessions (
    session_id   TEXT PRIMARY KEY,
    machine_id   TEXT NOT NULL,
    phase        TEXT NOT NULL,         -- scan / pull / apply_local / commit / backup
    status       TEXT NOT NULL,         -- running / failed / done
    started_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- 同步日志
CREATE TABLE sync_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp  TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    level      TEXT NOT NULL,
    action     TEXT NOT NULL,
    path_key   TEXT,
    detail     TEXT NOT NULL
);
```

### 本地配置、缓存与本机备份

本地状态拆成三部分：

- 默认启动配置：`C:\.usbsync\machine.json`
  - `machine_id`
  - `display_name`
  - `last_work_root`
  - `backup_dir`
  - `bound_device_id`
  - `bound_volume_id`
- 工作文件夹保留目录：`<work_root>\.usbsync-local\`
  - 只保存和当前工作目录强相关的扫描缓存与日志
- 本机数据库备份：默认放在 `C:\.usbsync\backup\`
  - `C:\.usbsync\backup\USBSync-latest.db`
  - `C:\.usbsync\backup\USBSync-prev.db`
  - 用户可改到别的本地目录

其中：

- `C:\.usbsync\machine.json` 用于程序启动时自动带出这台电脑上次使用的工作文件夹
- `.usbsync-local\` 永远不参与同步
- 数据库备份不再放在工作文件夹里，避免工作目录整体被删时备份一起丢失

工作目录相关文件：

- `<work_root>\.usbsync-local\scan_cache.json`
  - 上次扫描结果，至少保存 `path_key`、`display_path`、`kind`、`size`、`mtime_ns`、`md5`、`last_revision`
- `<work_root>\.usbsync-local\logs\`
  - 本机运行日志与最近一次错误详情

本地缓存的作用是减少重复算 hash，也用来弥补“有些工具会把 mtime 改回去”的场景。  
本机数据库备份只用于灾难恢复，不参与日常同步判断，也不在后台静默替代 U 盘库。  
程序启动时先读取 `C:\.usbsync\machine.json`，用其中保存的 `last_work_root` 直接加载上次工作目录；若不存在或失效，再要求用户重新选择。  
若工作文件夹所在卷不可写，则不进入同步，只提示用户先换到可写目录。

### 路径规范化规则

- 库内只保存相对 `work_root` 的路径，不保存盘符和绝对路径
- 比较时使用规范化后的 `path_key`：分隔符统一为 `/`，按 Windows 语义做不区分大小写比较
- `display_path` 仅用于界面显示与日志，保留用户更熟悉的写法
- 尾部点、尾部空格、保留名、非法字符、超长路径在扫描阶段直接拒绝并提示
- 目录创建按“先父后子”，目录删除按“先子后父”，避免顺序错误
- `.usbsync-local\` 在进入扫描前就排除，不参与变更比较

### 本机灾备副本

- 每次同步完整成功后，先做数据库完整性检查，再刷新本机 `C:\.usbsync\backup\USBSync-latest.db`
- 刷新前把上一份 `C:\.usbsync\backup\USBSync-latest.db` 轮换为 `C:\.usbsync\backup\USBSync-prev.db`
- 默认目录为 `C:\.usbsync\backup\`，用户可改为其它本地目录
- 备份默认开启；若用户关闭，需要明确提示会失去“U 盘损坏后从本机恢复”的能力
- 刷新备份前先检查目标目录剩余空间；若空间不足，把“备份未更新”作为显著警告显示在结果摘要里
- 发布包需要额外保留一份独立的 `USBSync.exe` 拷贝或压缩包，供 U 盘损坏后复制到新盘
- 恢复不在程序内执行；线下恢复时由用户退出程序后，手动用本机备份覆盖 U 盘上的 `USBSync.db`
- 线下覆盖恢复后，程序下次启动会把当前库视为新的恢复点，生成新的 `device_id` 并要求两台机器重新确认绑定，避免旧盘和新盘并存时误连

### 高效同步策略

同步流程（用户在当前 U 盘上启动程序并点击“同步”时）：

1. 加载本机启动配置
   - 启动时先读取本机保存的启动配置，直接带出上次工作文件夹和备份目录
   - 若记录的工作文件夹失效，则要求用户重新选择；重新确认后覆盖更新本机启动配置

2. 定位当前 U 盘并校验 `device_id`
   - 程序只使用“自身可执行文件所在的可移动盘”作为当前同步介质，不扫描其它盘
   - 只有“程序运行于可移动盘 + 当前盘存在 `USBSync.db` + `device_id` 匹配 + 若系统可取到则 `volume_id` 匹配”同时满足，才允许开始同步
   - 若当前盘还未初始化，则主窗口提供“初始化当前 U 盘”按钮；初始化完成前不允许执行同步

3. 读取 `machine_state.last_seen_revision`
   - 后续所有拉取操作都基于 `revision`，不基于机器时间

4. 快速扫描本地文件夹
   - 优先收集 `path + kind + size + mtime_ns`
   - 结合本地 `scan_cache` 判断哪些对象需要进一步计算 MD5
   - 首次绑定、缓存损坏、用户手动“完整复扫”、或达到周期校验阈值时，执行全量校验而不是仅靠缓存判断

5. 生成本地变更集
   - 新对象：`add` / `mkdir`
   - 内容变化：`modify`
   - 本地消失：`delete`
   - 重命名：按“旧路径 delete + 新路径 add”处理

6. 拉取远端变更集
   - 查询 `change_log` 中 `revision > last_seen_revision` 的记录
   - 不使用 `mtime`、`updated_at` 决定先后顺序

7. 以共同基线做三方判断
   - 对每个路径，基于 `base_revision` 判断“本地是否改过”“远端是否改过”
   - 能自动判断的自动处理，不能安全判断的转入冲突规则

8. 应用结果
   - 本地写文件时使用临时文件 + 原子替换
   - U 盘写库时使用单事务，并记录当前 `sync_session` 所处阶段
   - 同机只允许一个同步实例；若数据库正忙或已有同步任务运行，只提示等待或稍后重试，不并发写入
   - 任一步失败都不写入“成功”状态；下次手动启动同步时重新计算并重试

9. 提交后更新状态
   - 只有在本地应用已完整完成后，才更新 `machine_state.last_seen_revision`
   - 刷新本地 `scan_cache` 与 `machine_registry.last_seen_at`
   - 写入 `sync_log` 和本地日志

10. 刷新本机灾备副本
   - 仅在同步成功且完整性检查通过后刷新本机数据库备份
   - 若备份刷新失败，只记录告警，不回滚已经完成的同步结果

> **效率关键**：如果工作文件夹有 10000 个文件但只改了 3 个，扫描阶段不会读取所有文件内容，真正读写内容的只会是变更对象和冲突对象。

### 详细进度表

同步时主窗口必须持续显示详细进度表，至少包含：

- 阶段：扫描 / 拉取 / 合并 / 写本地 / 写数据库 / 备份
- 当前对象：正在处理的路径
- 动作：读取 / 写入 / 删除 / 冲突副本 / 跳过
- 已完成 / 总数
- 已传输字节
- 状态：进行中 / 成功 / 失败 / 警告
- 说明：最近一条细节信息

若某阶段开始时总数还未知，允许先显示当前阶段、当前对象和已完成数量；等统计完成后再补全总数。

### 同步恢复与安全重试

- 每一轮同步都生成 `session_id`，并把阶段写入 `sync_sessions`
- `last_seen_revision` 是最后提交的光标，不是“已经开始处理”的标记；在本地应用完成前绝不前移
- 若程序崩溃、U 盘意外拔出或系统断电，下次启动先读取未完成会话，做只读检查，再决定恢复或重跑
- 可重复重放的步骤必须得到相同结果，不允许因为重试而再生成一份新的冲突副本
- 冲突副本命名必须基于来源机器与 `revision`，不能依赖随机值或当前时间

### 工作文件夹切换规则

- 一台电脑默认只绑定一个工作文件夹
- 用户主动更换工作文件夹时，必须进入“更换工作目录”流程，不能静默替换
- 更换完成后，当前 U 盘库会把同步内容整体更新为新工作目录的当前状态，并把 `workspace_generation` 加一
- 同时在 `change_log` 中写入 `workspace_reset` 标记，作为跨机器可见的目录切换事件
- 另一台电脑下次同步时，若发现 `workspace_generation` 已变化，先明确提示“需要改用新的工作目录”
- 若用户按提示切换到新工作目录，则按新的目录继续同步
- 若用户无视提示继续对旧工作目录执行同步，则程序会先明确二次确认；确认后，旧目录会被清空并替换成当前同步集的完整内容

### 删除墓碑与内容清理

- 删除时不直接删掉 `entries` 行，而是把该路径标记为 `deleted = 1`
- 同时在 `change_log` 里追加一条 `delete` 记录，作为可传播的删除事件
- 删除后的内容块只有在没有 `entries`、冲突副本、未完成同步会话引用，且所有 active 机器都越过相关版本后，才允许清理
- 删除墓碑不能立刻删；只有当所有 active 机器的 `last_seen_revision` 都超过该删除版本后，才允许清理墓碑
- 只有当用户明确把某台旧机器标记为不再使用时，才可从 active 改为 retired；retired 机器不再阻塞墓碑清理
- 数据库不在每次同步后自动压缩；只有当可回收空间超过阈值时，才在空闲时做一次显式压缩，避免为回收少量空间频繁重写整个库

### 大文件处理

- 文件 > 64MB 自动分块存入 `blobs` 表，每块 16MB
- `blob_id` 按完整内容的 MD5 生成，相同内容不重复落盘
- 仅保留当前 `entries` 仍引用的内容块，不做历史版本长期保留
- 分块读写时把当前进度写入主窗口进度表

### 已知限制

- 依赖 `size + mtime_ns + 本地缓存` 进行首轮筛选，极少数“内容变化但时间戳被完全复原且缓存缺失”的场景仍可能需要全量复扫才能发现
- v1 不做跨目录移动检测，也不尝试自动合并文本差异
- 遇到文件被占用、权限不足、路径非法、超长路径时，优先保留现状并提示，不强行覆盖
- v1 的日常验收按“两台 active 机器”设计；历史机器记录会保留，但不作为功能扩展承诺
- v1 只提供手动同步，不提供后台自动同步

---

## U盘数据布局

```text
U盘根目录/
├── USBSync.exe         # 程序本体
└── USBSync.db          # 单个 SQLite 数据库文件（包含同步状态、内容与日志）
```

首次初始化时自动创建。  
用户正常看到的同步介质最终只需要这两个文件。

同步过程中允许 SQLite 短暂创建辅助文件以保证写入安全；同步完成、正常退出或恢复完成后，U 盘最终只保留 `USBSync.db`。

### U盘身份识别

程序启动后，只认“当前可执行文件所在的可移动盘”为目标同步盘。

首次初始化时，在 `USBSync.db` 内写入 `device_id` 与 `created_at`。若系统能取到卷序列号或等价盘标识，也一并记住。后续只有同时满足以下条件才允许同步：

- 程序运行于可移动盘
- 当前盘存在 `USBSync.db`
- 库内 `device_id` 与本地记录一致
- 若系统可取到，则卷序列号或等价盘标识也一致

若程序不是从可移动盘启动、当前盘缺少 `USBSync.db`、或 `device_id` 不匹配，则主窗口只显示原因与可执行操作，不进入同步。

### 首次绑定、重绑与恢复

- 首次绑定时，生成稳定的 `machine_id`，并要求用户填写可修改的显示名称 `display_name`
- `display_name` 只用于界面、日志和冲突文件名；用户后续可以修改，修改后不影响同步历史
- 同一 U 盘内，active 机器的 `display_name` 必须唯一；若重名，视为未配置完成
- 丢失本机启动配置、换电脑或重装后，程序先要求用户选择工作目录，再只读检查该目录中的 `.usbsync-local\` 与当前 U 盘库，随后让用户选择“接管已有机器”或“注册为新机器”
- 本机启动配置默认写到 `C:\.usbsync\machine.json`；程序启动时优先从这里读取上次工作目录和备份目录
- 某台电脑不再使用时，需要用户手动把它标记为 retired；程序不做自动超时退役
- 从本机备份恢复时，不在程序里直接执行；用户退出程序后，线下把备份库覆盖到 U 盘 `USBSync.db`，再重新启动程序
- 覆盖恢复前，文档与界面提示必须明确显示：将覆盖当前 U 盘数据库、当前绑定关系需要重新确认、恢复后另一台电脑需要重新核对工作目录
- 数据库损坏时，默认先做只读检测并保留损坏库副本；未得到用户确认前，不自动重建空库

---

## 项目结构

```text
D:\dev\usbsync\
├── cmd\usbsync\
│   └── main.go                    # [NEW] 入口
├── internal\
│   ├── sync\
│   │   ├── engine.go              # [NEW] 同步编排
│   │   ├── merge.go               # [NEW] 三方判断与变更合并
│   │   ├── conflict.go            # [NEW] 冲突命名与冲突记录
│   │   ├── recovery.go            # [NEW] 会话记录与失败恢复
│   │   └── progress.go            # [NEW] 进度事件与主窗口回传
│   ├── db\
│   │   ├── schema.go              # [NEW] SQLite 建表与迁移
│   │   ├── store.go               # [NEW] 数据库读写
│   │   └── backup.go              # [NEW] 本机灾备副本与库体积维护
│   ├── fileutil\
│   │   ├── fileutil.go            # [NEW] 扫描、分块、MD5、安全写入
│   │   └── path.go                # [NEW] Windows 路径规范化与校验
│   ├── fileindex\
│   │   └── cache.go               # [NEW] 本地扫描缓存
│   ├── usb\
│   │   └── current_drive.go       # [NEW] 当前 U 盘定位与校验
│   ├── config\
│   │   └── config.go              # [NEW] 本机启动配置、备份目录与 `.usbsync-local` 读写
│   └── ui\
│       ├── main_window.go         # [NEW] 主窗口与按钮
│       ├── progress_table.go      # [NEW] 详细进度表
│       ├── results.go             # [NEW] 结果摘要与冲突清单
│       └── dialogs.go             # [NEW] 初始化、恢复、错误对话框
├── assets\icon.ico                # [NEW] 窗口图标
├── go.mod                         # [NEW]
├── build.bat                      # [NEW]
└── README.md                      # [NEW]
```

---

## 主窗口与手动同步

```text
主窗口:
  当前U盘:         [E:\] [已识别 / 未初始化 / 身份不匹配 / 不可用]
  本地工作文件夹: [自动加载的目录] [浏览...]
  显示名称:       [办公室]
  本机备份目录:   [C:\.usbsync\backup] [更改...]
  保留本机灾备副本: [开/关]
  [初始化当前U盘] [同步] [打开备份目录]

  结果摘要:
    上次同步时间 / 本次新增 / 修改 / 删除 / 冲突 / 失败

  详细进度表:
    阶段 | 当前对象 | 动作 | 已完成/总数 | 已传输字节 | 状态 | 说明

  底部操作:
    [查看结果] [打开冲突目录] [管理机器] [导出日志] [关闭]
```

### 状态与进度显示

主窗口至少区分 5 种状态：

- 未配置
- 当前 U 盘不可用
- 同步中
- 同步成功
- 同步失败 / 有冲突

同步开始时，“同步”按钮置灰，避免重复触发；同步完成后恢复。

发生以下情况时，主窗口必须给出明确提示：

- 冲突
- 文件被占用
- 数据库不可写
- U 盘意外拔出
- 当前盘身份不匹配

提示内容至少包含四项：

- 出了什么问题
- 哪些文件受影响
- 下一步该怎么处理
- 用户现在可以直接点什么

高频问题需要给出明确操作入口：

- 冲突：打开冲突目录 / 查看冲突详情
- 文件被占用：重试 / 稍后再试
- 当前盘不可用：查看原因 / 初始化当前 U 盘 / 重新从正确 U 盘启动程序
- 数据库损坏：只读检查结果 / 打开备份目录 / 查看线下覆盖步骤

进度表必须持续刷新：

- 当前阶段
- 当前路径
- 累计处理数量
- 累计读写字节
- 最近一条结果

### 日志、关闭与重入规则

- 日志分两份
  - U 盘数据库内的 `sync_log`：保留跨机器同步历史
  - `<work_root>\.usbsync-local\logs\`：保留本机错误细节
- `sync_log` 需要保留上限；默认仅保留最近 180 天且最多 5000 条，支持导出后清理
- 点击“查看结果”时，默认显示最近一次同步结果、冲突清单和失败原因
- 若本机备份未更新，结果摘要必须以显著警告显示“当前备份不是最新”
- 结果页需要能直接打开冲突目录、显示原路径与冲突副本来源机器
- 同步进行中再次点击“同步”时，不启动第二个任务，只提示“正在同步中”
- 同步进行中点击“关闭”时，不直接强退；默认等待当前写入结束后退出
- 若用户坚持退出，也只能在事务安全结束后退出，不允许半写状态退出
- 同一台机器只允许一个程序实例；若用户重复启动，聚焦已有窗口并退出新实例

### 未配置状态

- 未配置完成时，程序仍可打开主窗口，但不允许开始同步
- 未配置完成时，“同步”按钮置灰，工作文件夹选择与初始化按钮保持可用
- 显示名称可以随时修改，但不会改变该机器的稳定身份

以下情况都视为未配置完成：

- 未从可移动盘启动程序
- 当前 U 盘未初始化，且用户尚未完成初始化
- 未选择工作文件夹
- 工作文件夹不存在
- 工作文件夹所在卷不可写
- 显示名称为空
- 显示名称与同一 U 盘内 active 机器重名
- 曾绑定的 `device_id` 丢失且用户未重新确认

---

## 冲突处理

### 自动处理规则

| 共同基线后的本地变化 | 远端变化 | 结果 |
|----------------------|----------|------|
| 无变化 | add / modify / mkdir | 拉取远端 |
| 无变化 | delete | 本地执行删除 |
| add / modify / mkdir | 无变化 | 上传本地变化 |
| delete | 无变化 | 上传删除墓碑 |
| delete | delete | 直接确认删除 |
| modify | delete | 保留修改版，记录警告，不自动删掉修改版 |
| delete | modify | 保留修改版，记录警告，不传播本地删除 |
| modify | modify | 视为冲突，保留两份 |
| file / dir 类型冲突 | 任意 | 视为冲突，保留两份并提示 |

### 冲突落地规则

- 冲突时不静默覆盖
- 当前已在 U 盘 `entries` 中占用原路径的版本保留原路径
- 新冲突版本另存为 `原文件名 (conflict-显示名-r<revision>)` 后缀
- 显示名称进入文件名前要先做净化与截断；若仍可能撞名，再附加固定短 ID
- 同时写入 `sync_log`
- 主窗口提示用户查看冲突清单

### 说明

- “一端删除 + 另一端修改”统一按“保留修改版”处理，优先避免数据丢失
- v1 不做自动文本合并，不尝试行级 merge
- v1 不做 rename 检测；重命名由“旧路径删除 + 新路径新增”自然表现

---

## 兼容性目标

目标平台保持为 Win10、Win11，以及 Win10 以上内核的 WinPE 64 位环境。

实现阶段需要同时满足两点：

- 核心功能必须在 Win10 / Win11 / WinPE 上可运行，包括主窗口、手动同步、进度表、U 盘校验与恢复
- WinPE 模式不要求托盘、自启动、系统通知或用户目录；只要手动同步流程与结果正确即可

WinPE 支持需要额外满足：

- 这里的 WinPE 指“基于 Win10 以上内核、带基本图形界面、可运行标准窗口程序”的环境
- 程序从 U 盘直接启动，不依赖安装
- 不依赖 `APPDATA`、`LOCALAPPDATA`、当前用户启动项或后台常驻能力
- 需要有可写的 `C:` 盘用于保存 `C:\.usbsync\machine.json`；数据库备份默认写到 `C:\.usbsync\backup\`，也可改到别的本地目录
- 如果 WinPE 中本地目标卷不可写，主窗口明确提示不能同步
- 通知能力以窗口内状态、对话框和进度表为基线，不依赖系统托盘或 toast

---

## Verification Plan

### 编译

```bash
go build -ldflags="-H windowsgui" -o USBSync.exe ./cmd/usbsync/
```

### 单元测试

```bash
go test ./internal/... -v
```

重点覆盖：

- `revision` 拉取与应用顺序
- 稳定机器身份与显示名称改名
- 路径规范化与大小写改名
- 删除墓碑传播
- active / retired 机器对墓碑清理的影响
- 冲突命名规则
- SQLite 增量读写
- 同步会话恢复与安全重试
- 大文件分块与内容清理
- 本地扫描缓存命中 / 失效场景
- 本机数据库备份刷新、校验与恢复流程
- 单实例约束、数据库忙时处理与日志保留上限
- `.usbsync-local\` 排除规则
- 详细进度事件的顺序与内容
- 本机启动配置加载与失效后的重新选择
- 工作目录切换后的 `workspace_generation` 传播

### 兼容性验证

需要分别在 Win10、Win11、WinPE（Win10+ 内核）真机或等效环境验证以下项目：

- 程序可直接从 U 盘启动并打开主窗口
- 主窗口可自动加载上次工作文件夹，必要时也可重新选择
- 主窗口可保存显示名称、备份目录并再次读取
- 点击“同步”后，详细进度表会持续刷新
- 从当前 U 盘初始化、同步都可正常触发
- 同步中拔出 U 盘后，窗口提示明确且下次重新插入后可恢复
- 中文路径、空目录、只读文件、长路径可被正确处理或正确报错
- 同步成功、失败、冲突提示都可见且可理解

### 场景验收

至少覆盖以下场景：

1. 首次配置后，电脑 A 新建文件，电脑 B 成功拉取
2. 电脑 A 修改文件，电脑 B 未改动，B 下次同步后得到最新版
3. 电脑 A 与电脑 B 轮流修改同一文件，最终结果符合冲突规则
4. 一端删除、另一端未改动，删除可正确传播
5. 一端删除、另一端修改，保留修改版并给出提示
6. 两端都删除同一路径，不产生伪冲突
7. 同步中途拔出 U 盘，程序不会把本地和数据库都写坏，重新插入后可恢复
8. 文件被占用、权限不足、路径失效时，用户能看到明确错误
9. 数据库损坏或无法打开时，不继续同步，并提示只读检查、打开备份目录按线下恢复步骤处理或经确认后重建
10. 同步进行中重复点击“同步”不会产生并发任务
11. 未配置完成时不会误同步
12. 从错误 U 盘或非可移动盘启动程序时，不会误对其它盘同步
13. Win10、Win11、WinPE 上都能完成一轮真实的手动同步流程
14. 修改显示名称后，不丢失该机器原有同步历史
15. 仅大小写变化的改名不会产生伪冲突或重复路径
16. 丢失本机启动配置后，可只读识别旧库并安全重绑
17. 线下用本机备份覆盖当前 U 盘数据库后，两台机器重新确认绑定并继续同步
18. 数据库忙、程序双开时，不会出现并发写入
19. 磁盘空间不足时，不进入“部分成功”状态
20. 辅助文件残留时，启动后能先恢复检查而不是直接继续同步
21. WinPE 下不依赖托盘、自启动和用户目录，也能完成同步
22. 本地保留目录 `.usbsync-local\` 不会被当成普通文件同步
23. 更换工作文件夹后，另一台电脑会先收到“需要切换工作目录”的明确提示
24. 无视工作目录切换提示并继续同步时，旧目录会被确认后清空并替换成新内容
25. 手动把某台旧机器标记为 retired 后，该机器不再阻塞删除清理

### 发布前检查

- `README.md` 写清从 U 盘启动、手动同步、WinPE 要求、已知限制和冲突文件命名规则
- `README.md` 写清显示名称可改但机器身份固定、本机启动配置默认位置 `C:\.usbsync\machine.json`，以及工作目录自动加载规则
- `README.md` 写清本机灾备副本默认在 `C:\.usbsync\backup\`、可修改目录，以及恢复时采用线下覆盖方式
- `README.md` 写清 `.usbsync-local\` 是本机保留目录，不参与同步
- `README.md` 写清更换工作文件夹会传播到另一台电脑，并说明“忽略提示会清空旧目录”的后果
- 发布物需要额外保留一份独立的 `USBSync.exe` 拷贝或压缩包，供新 U 盘恢复时使用
- `build.bat` 固定 Go 1.22.x 构建环境
- 首个可交付版本至少做一次双机实测，而不只依赖单元测试
