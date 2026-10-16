# 开发与实现细节

同一套 Go 后端和 Wails 界面，管理 Windows、macOS 和 Linux 的 OpenConnect 进程。首次启动自动按系统及架构下载便携核心、校验 SHA-256、解压到应用目录；不运行安装器，不执行 brew/apt/dnf/pacman/zypper，也不要求用户安装 OpenConnect 或解压软件。

默认连接 `https://vpn.cuhk.edu.cn`、认证组 `CUHK(SZ)`，仅转发 `10.20.0.0/16`。网段、学校账号、服务器和 DNS 策略可在界面设置。

## 使用

Windows 程序：`build/bin/CuhkVpn.exe`（x64）或 `build/bin/CuhkVpn-arm64.exe`（ARM64）。将应用放在可写目录并启动，等待便携核心准备完成，然后输入学校账号和 VPN 密码连接。下载核心不需要管理员权限；连接时由系统授权配置网卡和路由。

- 首次使用自动下载；已有完整且校验通过的核心时可离线复用。
- 默认界面为经典绿屏 CRT：紧凑窗口、扫描线、轻微荧光和内置点阵图标。连接、设置、日志分别展示；动效跟随真实的核心准备、系统授权和连接状态，遵循系统的「减少动态效果」设置。
- 「下载 / 修复便携核心」可重新检查和修复损坏文件。
- 支持取消连接、正常断开、分流路由检查、实时日志；密码可选择随软件目录保存或使用系统凭据存储。
- 最小化保持连接；关闭窗口后隐藏到托盘。托盘菜单支持显示主界面、连接/取消/断开和正常退出。退出时等待隧道与路由清理，超时则保留进程并恢复窗口供重试。Linux 桌面没有可用 StatusNotifier 托盘服务时，关闭窗口仍正常断开后退出。
- 已有 OpenConnect 会话时拒绝创建第二条连接。原 Windows C# 客户端持有同一个启动锁，需先退出原客户端。
- 自定义核心路径为可选高级设置。默认始终使用应用目录中的便携核心，不自动查找或依赖系统 OpenConnect。

## 目录及下载来源

```text
应用目录/
  CuhkVpn.exe                  # 或 Linux 程序 / macOS 的 CuhkVpn.app
  cores/
    .cache/<SHA256>            # 已校验的上游包，可离线复用
    windows-amd64/<版本-清单摘要>/
    windows-arm64/<版本-清单摘要>/
    darwin-amd64/<版本-清单摘要>/
    darwin-arm64/<版本-清单摘要>/
    linux-amd64/<版本-清单摘要>/
    linux-arm64/<版本-清单摘要>/
```

Windows/Linux 使用可执行文件所在目录；macOS 放在 `.app` 旁边的 `cores` 目录，避免下载操作修改已签名的 `.app` 内容。目录不可写时会给出错误，需将整个应用移到可写位置。

| 系统 / 架构 | 固定核心及依赖来源 | 便携运行方式 |
| --- | --- | --- |
| Windows amd64 | 官方 OpenConnect 9.21 NSIS 包 | 仅作为数据解压，保留 EXE、DLL、Wintun、官方 JScript；不运行安装器 |
| Windows ARM64 | 同一官方 x64 核心 | 明确使用系统的 x64 仿真；上游没有原生 ARM64 核心，未声称这是 ARM64 二进制 |
| Windows 386 | 官方 OpenConnect 9.21 x86 包 | 同上；GUI 发行构建主要针对 amd64 / ARM64 |
| macOS amd64 / arm64 | Homebrew 官方 9.21 bottle 及其确切运行库版本 | 直接下载 tar.gz，不调用 Homebrew；将动态库重定位到 `@loader_path`，由 Go 重建 SHA-256 临时签名，不调用 codesign 或要求安装 Xcode；最低 macOS 14 |
| Linux amd64 / arm64 | Alpine v3.24 的 OpenConnect 9.12-r2 及运行库 | 只解压 APK 数据，不执行包脚本；包含 musl 加载器、动态库、CA 和 ip 工具，通过自带加载器运行，不依赖宿主的 OpenConnect 或同版本动态库 |

路由脚本来自固定的 OpenConnect vpnc-scripts 提交。Windows 的解压工具来自固定的官方 7-Zip 包：先由 Go 解出 `7z.exe` / `7z.dll`，仅运行解压命令，完成后移除临时工具；用户无需安装 7-Zip。

所有下载 URL、版本和 SHA-256 固定在 `internal/core/sources.json`。每个压缩包先校验再解压；不能通过校验时不会使用。解压限制路径及展开大小，不创建指向应用目录外的符号链接，不保留 setuid 权限，不执行包的安装脚本。运行库的链接会转换为包内普通文件。

新核心在同一文件系统的临时目录中完成准备、动态库闭包检查和原生 `--version` 自检，最后才替换正式目录。失败时保留旧目录。`bundle.json` 记录展开后文件的摘要；`origins.json` 记录上游来源。再次启动会检查这些文件，损坏时重新准备。

维护者可执行 `node scripts/lock-core.mjs` 从官方来源更新固定清单。此脚本只用于开发维护，用户运行 GUI 时不需要 Node.js。更新清单应重新跑原生测试并审查来源、版本和哈希。

上游及对应源码：[OpenConnect](https://gitlab.com/openconnect/openconnect)、[vpnc-scripts](https://gitlab.com/openconnect/vpnc-scripts)、[7-Zip](https://github.com/ip7z/7zip)、[Homebrew Core 配方](https://github.com/Homebrew/homebrew-core)、[Alpine aports](https://gitlab.alpinelinux.org/alpine/aports)。各组件保留其上游许可；源码仓库不包含第三方核心；普通构建首次使用从上游下载，便携分发包可预置已校验的核心及对应许可。

## 进程、分流与凭据

```text
普通权限的 Wails 界面
        ↓ Go 方法及事件
便携核心管理器、设置、系统凭据存储
        ↓ 受系统权限保护的本机管道 + 固定证书的 TLS 会话
同一可执行文件的提权助手
        ↓ stdin / 日志管道 / 正常退出信号
应用目录中的 OpenConnect + 分流包装器 + 官方路由脚本
```

Windows 通过 UAC、macOS 通过系统管理员授权、Linux 通过 pkexec 启动助手。Linux 仍需要桌面环境的 polkit 授权服务，GUI 本身仍需要系统 GTK/WebKit；这些是桌面/授权运行环境，不是 OpenConnect 安装要求。

Windows 包装器使用 JScript，macOS/Linux 使用 POSIX Shell；覆盖服务端推送的 IPv4 路由，禁止默认路由及合起来覆盖整个 IPv4 空间的网段组合，禁用 VPN IPv6，默认保留系统 DNS，隧道地址使用 /32。Windows 登录前固定网关经物理网卡访问，断开时仅删除本次新建的网关路由。

Windows 网关准备直接调用 IP Helper API 读取物理网卡和路由、添加临时主机路由并清理，无需启动 PowerShell。每次连接重新查询，按路由跃点与接口跃点之和选择物理网卡；已有冲突路由时拒绝覆盖，断开只清理本次成功创建且仍符合所有权标记的路由。日志显示登录前网络准备耗时（含 DNS 和网关路由）。

Windows 使用独立的 `CUHK VPN Go` 网卡名称。原 C# 客户端供绑定 VPN 网卡的应用使用的额外公网默认路由不在本版默认策略中。「检测分流」只检查 VPN 地址和指定网段的路由，不将公网网站的测试结果当成校园 VPN 状态。

正常断开使用 Windows Ctrl+C 或 Unix SIGINT，等待 OpenConnect 清理；不使用强制 Kill 作为常规断开方式。界面意外退出后，助手仍会尝试正常断开。网关路由清理失败时助手保留所有权并重试。

设置位于应用目录的 `data/settings.json`，其中不含密码；旧版用户配置仍可读取，保存设置时迁移到应用目录。在 macOS 上，`data` 与 `.app` 位于同一目录，不能修改已签名的应用包。

勾选「记住密码」，选择保存方式，再输入密码并点击「保存设置」或「连接 VPN」。以后密码框留空即可连接。切换保存方式后需重新输入一次密码，程序不会读取或自动迁移另一种保存方式中的密码。

- **随软件目录保存**：使用随机 256 位密钥和 AES-GCM 保存到 `data/passwords/`，密钥 `key.bin` 与加密文件一起保存；连接设置也在 `data/`。复制完整软件目录（macOS 需连同旁边的 `data`）到另一台电脑后，可以直接复用账号和密码。密钥有意随目录携带，持有完整目录的人能够解密密码；这不提供防止目录被盗后的密码保护。不要公开或提交 `data/`，分发给他人前应移除个人数据。单独复制可执行文件无法携带密码。不同系统或架构需要使用对应的 GUI 程序，核心仍按当前平台准备。
- **系统凭据存储**：使用 Windows Credential Manager、macOS Keychain 或 Linux Secret Service。换电脑后需重新输入密码；现有系统保存的密码继续使用原有条目。「清除所选方式的密码」只删除当前账号在所选方式中的条目。

密码通过本机会话和 OpenConnect stdin 传递，不写入命令行或路由脚本。日志脱敏后在内存保留最近 500 条。

旧 C# / PowerShell 凭据不会自动迁移。学校认证目前使用密码和非交互模式；验证码、浏览器 SSO 尚未实现。

## 构建及诊断

Go 1.25+、Wails CLI `v2.16.0`。前端使用静态资源，无需 npm 打包。

前端图标已作为本地 PNG 遮罩打包，运行时无需 CDN 或图标库。点阵资源源自已确认草案中的 Lucide 图标，保留 `frontend/icons/LICENSE.txt`。`scripts/export-crt-icons.cjs` 仅用于开发时从该草案重新导出，普通构建不需要 Playwright。

Windows：

```powershell
.\build.ps1 -Test
.\build.ps1 -Dev
```

macOS 安装 Xcode Command Line Tools 后执行：

```sh
sh build.sh
sh build.sh universal
```

Linux 构建需要 GTK/WebKit 开发库，例如 Ubuntu 24.04：

```sh
sudo apt-get install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
sh build.sh
```

以上开发依赖仅供构建 GUI，不用于安装 OpenConnect。`build.sh` 会检测 WebKit 4.1 并添加 `webkit2_41` 标签。macOS/Linux GUI 需在相应系统原生链接；`.github/workflows/build.yml` 包含原生构建与便携核心网络自检，运行结果可在仓库 Actions 中查看。

可独立验证核心准备流程，不启动界面或连接 VPN：

```sh
go run ./cmd/corefetch -dir ./build/core-check
```

发布程序也支持 `--prepare-core` 和 `--core-info`，前者准备应用目录中的核心，后者只读取检测结果。自动网络测试通过 `CUHK_NETWORK_TEST=1 go test ./internal/core -run TestNativePortableCore -v` 启用，仅运行 `--version`，不登录 VPN。

## 本次验证边界

- Windows：测试下载、校验、解压、`--version`、离线复用；回归测试正常退出、分流脚本、凭据脱敏和本机会话。release GUI 已重新构建。
- Linux：两种架构的真实包完成解压、ELF 架构及动态库闭包检查；x64 核心和随包 ip 工具在 Ubuntu WSL 中实际运行成功。ARM64 仍需原生执行验证。
- macOS：两种架构的真实 bottle 已解压，完成动态库闭包检查、重定位和纯 Go 临时签名，签名测试检查代码页摘要。当前 Windows 环境不能执行 macOS 二进制，需在 macOS 上验证原生准备与运行。
- Windows 已在本机完成真实学校 VPN 连接验证；原生网关路由的添加、复用和清理经过管理员权限测试。macOS/Linux 的真实 VPN 连接仍待对应系统验证。
