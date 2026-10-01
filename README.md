# VRC素材库

Windows 上的 VRChat 素材管理工具。它会扫描你下载的素材文件夹、Booth 已购和百度网盘分享，把它们整理成一个像商店一样能浏览、筛选的本地素材库，还能标出哪些素材正在被你的 Unity 工程使用。

单个 exe，双击就能用，有自己的窗口。所有数据只存在你自己的电脑上。

## 功能

- **扫描素材文件夹**：识别解压后的素材目录、`.unitypackage` 和压缩包，自动判断分类（素体 / 衣服 / 头发 / 配饰…）和适配的素体，多个版本会合并成一个素材。
- **工程使用情况**：分析 Unity 工程，标出每个素材在哪个工程里被用到了。
- **Booth 商品**：
  - 关联方式：文件夹名里的商品编号、素材里的 Booth 快捷方式、Booth 已购记录，或者按名称在 Booth 上搜索后手动挑选。
  - 显示商品图片、标签和完整的商品说明，说明可以一键翻译成中文。
  - 没有封面的素材会用 Booth 的主图补上。
- **同步 Booth 已购**：在单独的浏览器窗口里登录 Booth，读取已购列表、收到的礼物和订单。登录信息只保存在这个工具自己的浏览器配置里。
- **百度网盘**：填分享链接（提取码可以不填），读取分享里的文件列表；只存在网盘里的素材也能收录进来。
- **简体中文名**：每个素材名下面显示一行中文翻译（必应翻译，可以手动改）。
- **跳转**：商品页、订单页、网盘链接都用系统的默认浏览器打开。

## 安装

在 [Releases](../../releases) 里下载：

- `VRC素材库-安装-x.y.z.exe`：安装版，装在当前用户目录，不需要管理员权限。
- `VRC素材库-便携版-x.y.z.zip`：便携版，解压到任意文件夹就能用。

运行要求：Windows 10 / 11（64 位）。界面用的是系统自带的 WebView2；极少数没有 WebView2 的电脑会改用 Edge 的独立窗口。

第一次打开时会让你选择素材文件夹和 Unity 工程文件夹，之后在「设置」里可以修改。

## 数据存放位置

| 情况 | 位置 |
| --- | --- |
| 便携版（exe 旁边有 `library.json` 或 `portable.txt`） | exe 所在文件夹 |
| 安装版，或 exe 所在文件夹不能写入 | `%LOCALAPPDATA%\VRC素材库` |
| 窗口的 WebView2 缓存 | `%LOCALAPPDATA%\VRC素材库\WebView2` |

主要文件：

- `library.json`：素材库数据，包括你填的备注、链接和翻译缓存。
- `covers\`：封面缓存。
- `booth-profile\`：同步 Booth 已购时用的浏览器配置，删掉就等于退出 Booth 登录。
- `library.log`：运行日志。

## 从源码构建

需要 Go 1.24 或更新版本。第三方库已经放在 `third_party/` 里，构建时不需要联网下载依赖。

**在 Windows 上**：双击 `build.bat`，或者执行：

```bat
go test .
go build -trimpath -ldflags "-H windowsgui -s -w" -o dist\VRC素材库.exe .
```

**在 Linux / WSL 上交叉编译**，同时生成安装包和便携版（需要 NSIS 3 和 zip）：

```sh
sh packaging/build.sh
```

`rsrc_windows_amd64.syso` 里是图标、版本信息和高 DPI 清单，已经生成好了。改了 `app.rc`、`app.ico` 或 `app.manifest` 之后，需要重新生成：

```sh
x86_64-w64-mingw32-windres --preprocessor=cat -c 65001 -O coff -i app.rc -o rsrc_windows_amd64.syso
```

## 代码结构

| 文件 | 内容 |
| --- | --- |
| `main.go` | 启动、本地服务（`127.0.0.1:47821`）、窗口 |
| `window_windows.go` | 独立窗口（WebView2） |
| `api.go` | 界面调用的接口 |
| `store.go` | `library.json` 的读写 |
| `scan.go` / `usage.go` / `thumbs.go` | 扫描素材、分析工程使用情况、封面 |
| `booth.go` / `boothmatch.go` | Booth 商品信息、搜索与匹配 |
| `purchases.go` / `syncdriver.go` / `cdp.go` / `booth_scraper.js` | 同步 Booth 已购（Chromium 用 CDP，Firefox 用 WebDriver BiDi） |
| `pan.go` | 百度网盘分享读取 |
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
VRCLIB_BOOTH_BASE=http://127.0.0.1:47990 \
VRCLIB_PAN_BASE=http://127.0.0.1:47991 VRCLIB_BOOTH_WEB=http://127.0.0.1:47991 VRCLIB_BING_BASE=http://127.0.0.1:47991 \
VRCLIB_BROWSER=<chrome 路径> VRCLIB_HEADLESS=1 \
go run . --no-window --data ./testdata
```

## 说明

- 本工具和 BOOTH、pixiv、百度网盘、VRChat 都没有关系。它读取的是这些网站的网页和公开接口，网站改版后部分功能可能失效。
- 本工具不会上传你的数据。它只会连接这几个地方：Booth、百度网盘、必应翻译（谷歌翻译作为备用），以及你在设置里填写的代理。

## 第三方代码

| 项目 | 许可证 |
| --- | --- |
| [jchv/go-webview2](https://github.com/jchv/go-webview2) | MIT（有少量修改，见 `third_party/go-webview2/VRCLIB_PATCHES.txt`） |
| [jchv/go-winloader](https://github.com/jchv/go-winloader) | ISC |
| [golang.org/x/sys](https://go.googlesource.com/sys) | BSD-3-Clause |
| WebView2Loader.dll（Microsoft WebView2 SDK） | 见 `third_party/go-webview2/webviewloader/sdk/LICENSE.txt` |

Booth 已购页面的读取方式参考了 [BoothDownloader](https://github.com/Myrkie/BoothDownloader) 和 [booth-library-manager](https://github.com/yoshiki-0428/booth-library-manager)，百度网盘分享的读取方式参考了 [AList 的百度分享驱动](https://alistgo.com/guide/drivers/baidu.share.html)。
