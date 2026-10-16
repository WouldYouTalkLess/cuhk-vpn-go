CUHK VPN {{VERSION}} · Windows 便携版
================================

启动
1. 把完整文件夹放在可写位置，不要只复制单个 EXE。
2. Intel / AMD 64 位电脑：双击 CuhkVpn.exe。
3. Windows ARM64 电脑：双击 CuhkVpn-arm64.exe。
4. 在「设置」中填写自己的学校账号和 VPN 密码，返回「连接」页连接。
5. Windows 管理员授权用于创建 VPN 网卡和临时路由；请按系统提示操作。

默认连接
服务器：https://vpn.cuhk.edu.cn
认证组：CUHK(SZ)
分流网段：10.20.0.0/16
只有设置中的校园网段通过 VPN，其余流量直连。可在连接后点击「检测分流」。

便携核心
cores 已包含 x64 和 ARM64 目录对应的 OpenConnect 9.21、依赖 DLL 和路由脚本。
不运行安装器，不需要手动安装 OpenConnect。已有核心完整时无需首次下载核心。
Windows ARM64 GUI 使用原生 ARM64 构建，OpenConnect 核心目前使用系统 x64 仿真，
因此 ARM64 电脑需具有 x64 仿真支持。
桌面界面仍需系统具备 Microsoft Edge WebView2 运行环境。

账号与密码
此分发目录只包含空白配置，不含开发者的账号、密码、密钥、个人日志或测试数据。
首次使用需填写自己的账号和密码。
「记住密码」默认关闭；需要时可选择随软件目录保存或系统凭据存储。
使用随目录保存后，转发给他人前应重新使用干净分发目录，避免携带个人数据。

连接与退出
窗口最小化后继续保持连接。
关闭窗口后留在系统托盘，VPN 继续运行。点击托盘图标或菜单中的「显示主界面」可恢复窗口。
托盘菜单可连接、取消或断开 VPN；从「退出 CUHK VPN」正常退出，等待断开和路由清理。
同一时间只启动一个 GUI，不要同时运行两种架构的程序。
网络准备时间会显示在「日志」页。

目录
CuhkVpn.exe            Windows x64 GUI
CuhkVpn-arm64.exe      Windows ARM64 GUI
cores/                 两种 Windows 架构对应的便携核心及依赖
data/settings.json     空白默认配置
LICENSES/              第三方许可与来源说明
MANIFEST.sha256        发行文件 SHA-256 清单

此文件夹是 Windows 运行分发包；不包含 macOS/Linux GUI、源码和构建工具。
