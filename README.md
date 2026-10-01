# MioVRC素材托管工具（MioVRC_AssetManager）

Windows 上的 VRChat 素材管理工具。扫描本地素材文件夹、Booth 已购和百度网盘分享，整理成可以浏览、筛选的素材库，并标出哪些素材被 Unity 工程用到。

单个 exe，有自己的窗口，数据只保存在本机。网站：<https://miovrc.com/vrca/>

## 功能

- 扫描素材文件夹：识别解压后的文件夹、`.unitypackage` 和压缩包，判断分类（素体、衣服、头发、配饰……）和适配素体
- 同款合并：同一商品的多个下载（不同素体的包、PSD 包）合成一张卡片，按素体筛选时只显示对应版本
- 衣服风格标签：按 Booth 标签自动分类（JK、Sexy、H、可爱、女仆、成熟……），可自定义
- PSD 源文件：列出 PSD / CLIP / SAI 等文件和尺寸
- 工程使用：分析 Unity 工程，标出素材在哪些工程里用到
- Booth 商品：按文件夹名里的编号、Booth 链接、已购记录或名称搜索关联，显示图片、标签和说明（可翻译）
- Booth 页：点选分类、素体、风格标签逛 Booth（可多选），标出已购和素材库已有的，可以隐藏它们
- 内置页面：Booth 的商品页、登录、购物车、已购列表和闲鱼都在软件里打开（第二个 WebView2，登录保存在本机），可以直接购买、和卖家聊天
- 同步 Booth 已购：在内置页面登录 Booth，读取已购、礼物和订单
- 下载 Booth 已购：在软件里直接下载（也包括在内置 Booth 页面里点的下载），zip 自动解压到素材文件夹并入库
- 闲鱼：在内置页面搜索、聊天、下单；聊天里选中的网盘分享可以一键收进素材库
- 百度网盘：读取分享里的文件列表，只在网盘里的素材也能收录；合集分享按分类文件夹拆成多张卡片
- 新素材自动整理：软件开着时，素材文件夹里新放入的素材会自动扫描归类
- 更新提示：网盘分享每天、Booth 商品页每周检查一次变化
- 中文名：素材名下面显示中文翻译，可以手动改
- 检查更新：从本仓库的 Releases 下载并替换，更新后显示这一版的更新内容
- 反馈和建议：经 [FormSubmit](https://formsubmit.co) 发到作者邮箱

## 安装

在 [Releases](https://github.com/CokoIya/MioVRC_AssetManager/releases) 下载：

- `MioVRC_AssetManager-setup-x.y.z.exe`：安装版，不需要管理员权限；默认装在 C 盘以外的磁盘（例如 `D:\MioVRCA`），没有其他磁盘时装在 C 盘
- `MioVRC_AssetManager-portable-x.y.z.zip`：便携版，解压即用

需要 Windows 10 / 11（64 位）。界面使用系统自带的 WebView2，没有 WebView2 时改用 Edge 窗口。

1.6 之前叫「VRC素材库」，仓库是 `CokoIya/vrclib`。旧版本可以直接覆盖安装或在软件里更新，数据会保留。

## 数据存放位置

| 情况 | 位置 |
| --- | --- |
| 便携版（exe 旁边有 `library.json` 或 `portable.txt`） | exe 所在文件夹 |
| 安装版，或 exe 所在文件夹不能写入 | `%LOCALAPPDATA%\MioVRC_AssetManager`（旧版留下的 `%LOCALAPPDATA%\VRC素材库` 会继续使用） |
| 窗口的 WebView2 缓存 | 上面那个文件夹里的 `WebView2` |

主要文件：

- `library.json`：素材库数据，包括备注、链接和翻译缓存
- `covers\`：封面缓存
- `web-login\`：内置页面（Booth、闲鱼）的登录和缓存，删掉等于退出登录
- `booth-profile\`：没有 WebView2 时，Booth / 闲鱼页面所用的 Edge / Chrome 窗口的配置
- `booth-session.dat`：下载 Booth 已购用的登录信息（用 Windows DPAPI 加密，只有本机本用户能解开）
- `library.log`：运行日志

## 从源码构建

需要 Go 1.24 或更新版本。第三方库已经放在 `third_party/` 里，构建时不需要联网下载依赖。

**在 Windows 上**：双击 `build.bat`，或者执行：

```bat
go test .
go build -trimpath -ldflags "-H windowsgui -s -w" -o dist\MioVRC_AssetManager.exe .
```

**在 Linux / WSL 上交叉编译**，同时生成安装包和便携版（需要 NSIS 3 和 zip）：

```sh
sh packaging/build.sh
```

`rsrc_windows_amd64.syso` 里是图标、版本信息和高 DPI 清单，已经生成好了。改了 `app.rc`、`app.ico` 或 `app.manifest` 之后，需要重新生成：

```sh
x86_64-w64-mingw32-windres --preprocessor=cat -c 65001 -O coff -i app.rc -o rsrc_windows_amd64.syso
```

## 发布新版本

软件从本仓库的 Releases 检查更新（只看最新的正式版）：

1. 改版本号：`main.go` 的 `appVersion`、`app.rc`、`app.manifest`、`packaging/installer.nsi`，重新生成 `rsrc_windows_amd64.syso`（命令见上）。
2. 在 `CHANGELOG.md` 最上面写这一版的更新内容。它会编译进程序，更新后第一次打开时显示，也显示在「更新公告」里。
3. `sh packaging/build.sh`，在 `dist/` 里得到 `MioVRC_AssetManager-setup-x.y.z.exe` 和 `MioVRC_AssetManager-portable-x.y.z.zip`。
4. 新建 Release，标签写成 `v1.6.0` 这样的格式，上传这两个文件。Release 的说明会显示在软件的更新窗口里。
   - 软件下载便携版 zip，取出 exe 替换自己后重启；只有安装版时会下载并打开安装程序。
   - 附件名用英文，中文名在 GitHub 上可能被改掉。

## 代码结构

| 文件 | 内容 |
| --- | --- |
| `main.go` | 启动、本地服务（`127.0.0.1:47821`）、窗口 |
| `window_windows.go` | 独立窗口（WebView2） |
| `api.go` | 界面调用的接口 |
| `store.go` | `library.json` 的读写 |
| `scan.go` / `usage.go` / `thumbs.go` | 扫描素材、分析工程使用情况、封面 |
| `booth.go` / `boothmatch.go` | Booth 商品信息、搜索与匹配 |
| `boothdl.go` | 下载 Booth 已购、解压（含 Shift-JIS 文件名） |
| `boothshop.go` | Booth 页：标签转成 Booth 分类和搜索词，标出已购、已有 |
| `webpane.go` / `webpane_windows.go` | 内置页面：窗口里的第二个 WebView2（没有时用单独的 Edge / Chrome 窗口），经 127.0.0.1 上的 DevTools 端口控制 |
| `groups.go` / `psd.go` / `panparts.go` | 同款合并、「For_X」素体识别、衣服风格标签、PSD 统计、网盘分享内容分析 |
| `update.go` / `changelog.go` | 检查更新、下载替换、重启；更新公告 |
| `purchases.go` / `syncdriver.go` / `cdp.go` / `booth_scraper.js` | 同步 Booth 已购（Chromium 用 CDP，Firefox 用 WebDriver BiDi） |
| `pan.go` / `panitems.go` / `sync.go` | 百度网盘分享读取、合集拆分、定期检查变化 |
| `watch.go` | 监视素材文件夹 |
| `feedback.go` | 反馈和建议 |
| `translate.go` | 中文翻译 |
| `browser.go` / `sys_windows*.go` | 默认浏览器、系统相关功能 |
| `web/` | 界面（HTML / CSS / JS，编译时嵌进 exe） |
| `packaging/` | 安装包脚本和使用说明 |
| `test/` | 本地测试用的假 Booth / 网盘 / 翻译服务 |

## 测试

```sh
go test .
```

界面和联网功能可以接到 `test/` 里的假服务上测试，不碰真实账号：

```sh
python3 test/mockbooth.py 47990
python3 test/mockweb.py 47991
VRCLIB_BOOTH_BASE=http://127.0.0.1:47990 VRCLIB_BOOTH_DL=http://127.0.0.1:47990 \
VRCLIB_PAN_BASE=http://127.0.0.1:47991 VRCLIB_BOOTH_WEB=http://127.0.0.1:47991 VRCLIB_BING_BASE=http://127.0.0.1:47991 \
VRCLIB_XY_BASE=http://127.0.0.1:47991/xy \
VRCLIB_BROWSER=<chrome 路径> VRCLIB_HEADLESS=1 \
go run . --no-window --data ./testdata
```

## 说明

- 本工具和 BOOTH、pixiv、闲鱼、百度网盘、VRChat 都没有关系。它读取的是这些网站的网页和公开接口，网站改版后部分功能可能失效。
- 本工具不会上传你的素材数据。它会连接：Booth、百度网盘、必应翻译（谷歌翻译备用）、GitHub（检查更新），发送反馈时连接 FormSubmit，以及设置里填写的代理；内置页面打开的网站（Booth、闲鱼等）由页面自己连接。
- 内置页面开着时，会在 127.0.0.1 上开一个 DevTools 端口，软件靠它控制页面（后退、读取已购、下载按钮）。只有本机程序能连到它。

## 第三方代码

| 项目 | 许可证 |
| --- | --- |
| [jchv/go-webview2](https://github.com/jchv/go-webview2) | MIT（有少量修改，见 `third_party/go-webview2/VRCLIB_PATCHES.txt`） |
| [jchv/go-winloader](https://github.com/jchv/go-winloader) | ISC |
| [golang.org/x/sys](https://go.googlesource.com/sys) | BSD-3-Clause |
| WebView2Loader.dll（Microsoft WebView2 SDK） | 见 `third_party/go-webview2/webviewloader/sdk/LICENSE.txt` |

Booth 已购页面的读取方式参考了 [BoothDownloader](https://github.com/Myrkie/BoothDownloader) 和 [booth-library-manager](https://github.com/yoshiki-0428/booth-library-manager)，百度网盘分享的读取方式参考了 [AList 的百度分享驱动](https://alistgo.com/guide/drivers/baidu.share.html)。

A local desktop asset manager for VRChat avatar modding. Organize Booth purchases, local folders and Baidu Netdisk links (share codes & paths) as clickable cards. | VRChat改模素材管理工具：Booth购买记录、本地文件夹、百度网盘链接一站式管理 | VRChat改変素材管理ツール：BOOTH購入品・フォルダ・ネットドライブリンクを一括管理
关键词：VRChat 改模 素材管理 换装 Booth 百度网盘 素材库
キーワード：VRChat アバター改変 素材管理 BOOTH 購入品管理 衣装
