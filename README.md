# USBSync

USBSync 是一个面向 **Windows 10 及以上（含 WinPE 图形环境）** 的离线同步工具。  
程序放在任意目录即可运行，点击一次“同步”即可在多台电脑之间同步同一个工作文件夹内容。

---

## 主要特性

- 单文件 GUI 程序，手动打开、手动同步（无托盘、无自启动）
- 数据库固定放在程序目录：`USBSync.db`
- 本机配置固定放在程序目录：`usbsync.json`
- 支持多机轮换同步
- 支持冲突副本保留，避免覆盖丢失
- 同步时实时显示详细进度和进度条
- 每次同步后自动刷新本机数据库备份（`latest` / `prev`）
- 保存与恢复文件时，保留文件**创建时间**和**修改时间**

---

## 运行要求

- 系统：Windows 10 或更新版本（含 Win10 内核 WinPE）
- 文件系统：建议 NTFS
- 不兼容目标：Windows 7

---

## 快速开始

1. 运行 `USBSync.exe`
2. 设置：
   - 工作文件夹（必填）
   - 机器名称（可自定义）
   - 备份目录（默认 `C:\.usbsync\backup`，可改）
3. 首次使用点击 **初始化数据库**
4. 点击 **同步**
5. 在另一台电脑上打开同一个程序目录，设置本机工作文件夹后点击 **同步**

---

## 同步规则（简版）

- 一台机器对应一个工作文件夹
- 同步判断基于文件内容/删除记录，不依赖机器时钟
- 冲突时生成冲突副本，不直接覆盖本地改动
- 同机更换工作文件夹后，需要重新初始化该库

---

## 目录与文件说明

| 路径 | 作用 |
|---|---|
| `USBSync.exe` | 主程序 |
| `USBSync.db` | 同步数据库（程序目录） |
| `usbsync.json` | 本机配置（程序目录） |
| `C:\.usbsync\backup\USBSync-latest.db` | 最新备份 |
| `C:\.usbsync\backup\USBSync-prev.db` | 上一版备份 |

---

## 从源码构建

```powershell
go test ./internal/... ./cmd/...
go build -ldflags="-H windowsgui" -o USBSync.exe ./cmd/usbsync
```

也可直接执行：

```powershell
.\build.bat
```

---

## 仓库结构

```text
cmd/usbsync          程序入口
internal/app         业务流程
internal/db          数据库存取
internal/sync        同步决策与冲突处理
internal/ui          Windows 界面
internal/fileutil    文件扫描/写入/路径校验
packaging            图标与 manifest
```

---

## 许可证

本项目采用 MIT License，详见 [LICENSE](./LICENSE)。

