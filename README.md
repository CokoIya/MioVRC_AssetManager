# MioVRCA（MioVRC_AssetManager）

Windows 上的 VRChat 素材管理工具。扫描本地素材文件夹、Booth 已购和百度网盘分享，整理成可以浏览、筛选的素材库，标出哪些素材被 Unity 工程用到，并能一键导入工程。

单个 exe，有自己的窗口，数据只保存在本机。网站：<https://miovrc.com/vrca/>

作者：天川澪（VRC 同名）。QQ 交流群：<https://qm.qq.com/q/5d1fxbkYhy>　Discord：<https://discord.gg/CD8NVJwBA>

## 功能

- 扫描素材文件夹：识别解压后的文件夹、`.unitypackage` 和压缩包，判断分类（素体、衣服、头发、配饰……）和适配素体（文件名，加上 Booth 商品页的标题、标签和「対応アバター」段落）；素材旁边同名的图片当封面，散放着多个「包 + 同名图片」的文件夹按多个素材收录；「批量整理」可以一次给多个素材改分类、隐藏、不再收录
- 同款合并：同一商品的多个下载（不同素体的包、PSD 包）合成一张卡片，按素体筛选时只显示对应版本
- 衣服风格标签：按 Booth 标签自动分类（JK、Sexy、H、可爱、女仆、成熟……），可自定义
- PSD 源文件：列出 PSD / CLIP / SAI 等文件和尺寸
- 工程使用：分析 Unity 工程，标出素材在哪些工程里用到
- 工程页：每个 Unity 工程一张卡片（Unity 版本、上次打开、用到的素材），用对应版本的 Unity 一键打开；可选的封面插件（`com.miovrc.projectcard`，只在编辑器里运行）在打开工程、保存场景时给模型拍正面照当封面；卡片上的「摄影棚」装上摄影棚插件（`com.miovrc.studio`）并让 Unity 进入 Play 模式：拖骨骼摆姿势、调手势表情和视线、布光、取景拍照，照片存到「图片」文件夹的 `MioVRCA/<工程名>`，不改场景也不随模型上传。工程里的插件管理交给 VCC / ALCOM
- 一键导入 Unity 工程：自动解压素材里的压缩包（PSD 包、分卷、嵌套、带密码），解压完压缩包移到回收站，再把 unitypackage 直接写进工程（按 GUID 就地更新已有资源，重名不覆盖；不同素体的包按工程的素体自动选）。rar、7z、分卷用电脑上默认的解压软件（7-Zip、Bandizip、WinRAR、好压、NanaZip、PeaZip）
- 流水线页：工程 → 素材 → 装配 → 菜单 → 验收。工程 Assets 里导入过的素材自动列出（带素材库里的名字和类别），勾上要装的，按类别装到模型上（衣服互斥切换 + 部件开关 + 换色；头发单独互斥，素体自带的头发可切回；配饰开关默认显示；道具开关默认隐藏；素体在场景没有模型时放进新场景），再按玩家定的层级生成 Modular Avatar 菜单和图标。给工程装上流水线插件（`com.miovrc.pipeline`）和 [UnitySkills](https://github.com/Besty0728/Unity-Skills) 后，还能让 AI 接着做别的改模操作。AI 服务由玩家自己选：DeepSeek、通义千问、智谱 GLM、Kimi、ChatGPT 兼容（OpenAI 接口）、Claude 兼容（Anthropic 接口）；也可以不用 AI，按网格名字分组。AI 可以给头像拍照检查结果（穿模、错位、材质丢失），截图同时显示在验收记录里：模型能看图就直接把图发给它；看不了图的（DeepSeek）可以另设一个「看图模型」把画面描述给它；第一次用时自动测一次主模型能不能看图
- 一键创建基础工程：Unity 2022.3.22f1 的 VRChat 头像工程（工程设置取自 VRChat 官方模板），从各插件的官方 VPM 仓库解析并下载最新稳定版（VRChat SDK、Modular Avatar、lilToon、Avatar Optimizer、Gesture Manager、VRCFury，可勾选；复用 ALCOM / VCC 的缓存），可顺带导入素材库里的素体、装 Unity 插件并打开 Unity；新工程登记进 ALCOM / VCC 的工程列表。检测本机的 ALCOM、VCC、Unity Hub 和所需 Unity 版本
- Booth 商品：按文件夹名里的编号、Booth 链接、已购记录或名称搜索关联，显示图片、标签和说明（可翻译）
- Booth 页：点选分类、素体、风格标签逛 Booth（可多选），标出已购和素材库已有的，可以隐藏它们
- 内置页面：Booth 的商品页、登录、购物车、已购列表和闲鱼都在软件里打开（第二个 WebView2，登录保存在本机），可以直接购买、和卖家聊天
- 同步 Booth 已购：在内置页面登录 Booth，读取已购、礼物和订单
- 下载 Booth 已购：在软件里直接下载（也包括在内置 Booth 页面里点的下载），自动解压到素材文件夹并入库；可以「下载并导入」
- Gumroad 已购：在内置页面登录 Gumroad，读取已购（含已归档），和 Booth 已购一样显示成卡片、下载、解压、入库
- 闲鱼：在内置页面搜索、聊天、下单；聊天里选中的网盘分享可以一键收进素材库
- 百度网盘：读取分享里的文件列表，只在网盘里的素材也能收录；合集分享按分类文件夹拆成多张卡片
- 百度网盘下载：在内置页面登录百度网盘（登录加密保存在本机，可以更换账号、退出），分享先存进自己网盘的 `/MioVRCA`，再下载、解压、入库；可以只勾选分享里的部分文件或文件夹；断了可以接着下。速度取决于账号的会员等级
- 下载位置：默认是 C 盘以外剩余空间最大的盘的 `MioVRCdownload`（优先素材文件夹所在的盘），设置里可以改
- 新素材自动整理：软件开着时，素材文件夹里新放入的素材会自动扫描归类
- 更新提示：网盘分享每天、Booth 商品页每周检查一次变化
- 中文名：素材名下面显示中文翻译，可以手动改
- 检查更新：从本仓库的 Releases 下载并替换，更新后显示这一版的更新内容
- 反馈与建议：经 [FormSubmit](https://formsubmit.co) 发到作者邮箱

## 安装

在 [Releases](https://github.com/CokoIya/MioVRC_AssetManager/releases) 下载：

- `MioVRC_AssetManager-setup-x.y.z.exe`：安装版，不需要管理员权限；默认装在 C 盘以外的磁盘（例如 `D:\MioVRCA`），没有其他磁盘时装在 C 盘
- `MioVRC_AssetManager-portable-x.y.z.zip`：便携版，解压即用

需要 Windows 10 / 11（64 位）。界面使用系统自带的 WebView2，没有 WebView2 时改用 Edge 窗口。

1.6 之前叫「VRC素材库」，仓库是 `CokoIya/vrclib`；1.6 到 1.7.0 叫「MioVRC素材托管工具」。exe、仓库和数据文件夹的名字（`MioVRC_AssetManager`）没有变，旧版本可以直接覆盖安装或在软件里更新，数据会保留。在软件里更新的，桌面和开始菜单的快捷方式还是旧名字，重新运行一次安装包就会换成新的。

## 数据存放位置

| 情况 | 位置 |
| --- | --- |
| 便携版（exe 旁边有 `library.json` 或 `portable.txt`） | exe 所在文件夹 |
| 安装版，或 exe 所在文件夹不能写入 | `%LOCALAPPDATA%\MioVRC_AssetManager`（旧版留下的 `%LOCALAPPDATA%\VRC素材库` 会继续使用） |
| 窗口的 WebView2 缓存 | 上面那个文件夹里的 `WebView2` |

主要文件：

- `library.json`：素材库数据，包括备注、链接和翻译缓存
- `covers\`：封面缓存
- `web-login\`：内置页面（Booth、Gumroad、闲鱼、百度网盘）的登录和缓存，把它和 `web-session.dat` 一起删掉等于退出登录
- `booth-profile\`：没有 WebView2 时，Booth / 闲鱼页面所用的 Edge / Chrome 窗口的配置
- `web-session.dat`：内置页面里各网站「关掉浏览器就失效」的登录 Cookie 的备份（闲鱼的登录就是这种），启动时放回去，所以重启、更新后不用重新登录；同样用 DPAPI 加密，退出某个网站的登录时会删掉对应的部分
- `booth-session.dat`：下载 Booth 已购用的登录信息（用 Windows DPAPI 加密，只有本机本用户能解开）
- `gumroad-session.dat`：读取和下载 Gumroad 已购用的登录信息（同样用 DPAPI 加密）；在设置里退出 Gumroad 会删除它
- `baidu-session.dat`：下载百度网盘分享用的登录信息（只有网盘需要的几个 Cookie，同样用 DPAPI 加密）；在设置里退出百度网盘会删除它
- `ai.json`：流水线的 AI 设置（服务商、接口地址、模型、菜单层级、识图方式和视觉模型、每个模型是否支持识图、每个工程导入过的素材位置），不含 Key
- `shots/`：AI 拍的头像截图（验收记录里显示的那些，只留最近 100 张）
- `vpm/`：创建基础工程时下载的插件 zip 和仓库列表缓存
- `ai-key.dat`：各 AI 服务商和视觉模型的 API Key（用 DPAPI 加密，只有本机本用户能解开；不会回传给界面，界面只知道有没有保存和末四位）
- `library.log`：运行日志

流水线放进 Unity 工程的东西（点「移除」会删掉）：`Packages/com.miovrc.pipeline`、`Packages/com.besty.unity-skills`（工程自己已经有 UnitySkills 时不放也不删）、`UserSettings/MioVRCA/bridge`（软件和 Unity 之间传话用的文件）、`UserSettings/MioVRCA/shots`（拍照时的临时图片，软件读走就删）。生成的菜单图标在工程的 `Assets/MioVRCA/<头像名>/Icons`。

摄影棚放进 Unity 工程的东西（工程详情里点「移除摄影棚」会删掉）：`Packages/com.miovrc.studio`、`UserSettings/MioVRCA/studio`（摄影棚的设置和保存的姿势，还有软件请 Unity 打开摄影棚时留下的 `open.json`，Unity 读到就删）。照片不在工程里，在「图片」文件夹的 `MioVRCA/<工程名>`，移除时不删。

## 从源码构建

需要 Go 1.24 或更新版本。第三方库已经放在 `third_party/` 里，构建时不需要联网下载依赖。

**在 Windows 上**：双击 `packaging\build.bat`，或者在仓库根目录执行：

```bat
go test ./...
go build -trimpath -ldflags "-H windowsgui -s -w" -o dist\MioVRC_AssetManager.exe ./cmd/miovrca
```

**在 Linux / WSL 上交叉编译**，同时生成安装包和便携版（需要 NSIS 3 和 zip）：

```sh
sh packaging/build.sh
```

`cmd/miovrca/rsrc_windows_amd64.syso` 里是图标、版本信息和高 DPI 清单，已经生成好了。改了同一个文件夹里的 `app.rc`、`app.ico` 或 `app.manifest` 之后，需要重新生成：

```sh
cd cmd/miovrca
x86_64-w64-mingw32-windres --preprocessor=cat -c 65001 -O coff -i app.rc -o rsrc_windows_amd64.syso
```

## 发布新版本

软件从本仓库的 Releases 检查更新（只看最新的正式版）：

1. 改版本号：`internal/core/version.go` 的 `AppVersion`、`cmd/miovrca/app.rc`、`cmd/miovrca/app.manifest`、`packaging/installer.nsi`，重新生成 `rsrc_windows_amd64.syso`（命令见上）。
2. 在 `CHANGELOG.md` 最上面写这一版的更新内容。它会编译进程序，更新后第一次打开时显示，也显示在「更新日志」里。不需要告诉玩家的版本可以不写新条目（1.7.4 就沿用 1.7.3 的）：从 1.7.3 升上来的不再弹出，从更早版本升上来的看到的是 1.7.3 的条目。
3. `sh packaging/build.sh`，在 `dist/` 里得到 `MioVRC_AssetManager-setup-x.y.z.exe` 和 `MioVRC_AssetManager-portable-x.y.z.zip`。
4. 新建 Release，标签写成 `v1.6.0` 这样的格式，上传这两个文件。Release 的说明会显示在软件的更新窗口里。
   - 软件下载便携版 zip，取出 exe 替换自己后重启；只有安装版时会下载并打开安装程序。
   - 附件名用英文，中文名在 GitHub 上可能被改掉。

## 代码结构

仓库按 Go 项目的常见布局组织：`cmd/` 是程序入口，`internal/` 是各个功能包，其余是编译进程序的资源、打包脚本和测试用的假服务。

```text
cmd/miovrca/      程序入口
internal/         各功能包（见下表）
web/              界面（HTML / CSS / JS）
unityhelper/      自己写的 Unity 插件（C#）
unitykit/         随软件带的 UnitySkills
assets.go         把上面三个文件夹和 CHANGELOG.md 编译进 exe
packaging/        构建脚本、安装包脚本、使用说明
test/             本地测试用的假服务
third_party/      依赖库的副本
```

| 位置 | 内容 |
| --- | --- |
| `cmd/miovrca/` | 启动、本地服务（`127.0.0.1:47821`）、独立窗口（WebView2）。图标、版本信息和清单（`app.rc`、`app.ico`、`app.manifest`、`rsrc_windows_amd64.syso`）也在这里 |
| `internal/core/` | 所有包共用的底座：`library.json` 的数据和读写（`store.go`、`model.go`）、设置、后台任务（`tasks.go`）、日志、版本号（`version.go`）、各网站的地址（`sites.go`）、默认浏览器和其他系统相关功能（`sys_*.go`） |
| `internal/naming/` | 从文件名、文件夹名里认东西：显示名、分类、「For_X」素体识别、Booth 编号 |
| `internal/semver/` | 版本号和版本范围的比较 |
| `internal/archive/` | 压缩包：解压 zip（含 Shift-JIS 文件名）、分卷识别、嵌套解压、优先调用电脑上默认的解压软件（7-Zip / Bandizip / WinRAR / 好压 / NanaZip / PeaZip），移到回收站 |
| `internal/update/` | 检查更新、下载替换、重启；更新公告 |
| `internal/webpane/` | 内置页面：窗口里的第二个 WebView2（没有时用单独的 Edge / Chrome 窗口），经 127.0.0.1 上的 DevTools 端口控制；浏览器驱动（Chromium 用 CDP，Firefox 用 WebDriver BiDi） |
| `internal/translate/` | 中文翻译 |
| `internal/netdisk/` | 百度网盘分享读取、合集拆分、分享内容分析、和上次相比的变化 |
| `internal/booth/` | Booth 商品信息、搜索与匹配 |
| `internal/library/` | 素材库本身：扫描素材（`scan.go`）、分析工程使用情况（`usage.go`）、封面（`thumbs.go`）、同款合并和衣服风格标签（`groups.go`）、PSD 统计、已购和网盘分享对上素材（`purchases.go`、`automatch.go`）、界面显示用的卡片数据（`views.go`）、Booth 页（`boothshop.go`：标签转成 Booth 分类和搜索词，标出已购、已有）、刷新流程（`refresh.go`：扫描 → 使用情况 → Booth）、定期检查变化（`sync.go`）、监视素材文件夹（`watch.go`） |
| `internal/unity/` | Unity 工程：工程页（`projects.go`：Unity 版本、打开状态、找本机的 Unity 编辑器、工程封面插件）、摄影棚插件的安装、更新、移除和打开请求（`studio.go`）、一键导入（`unityimport.go`：读 unitypackage、按 GUID 写进工程、选素体版本）、自带的 VPM 解析器和一键创建基础工程（`vpm.go`、`projectnew.go`）、检测 ALCOM / VCC / Unity Hub（`toolchain_*.go`）、往工程里装 / 移除插件并和 Unity 传话（`unitybridge.go`） |
| `internal/purchases/` | Booth 和 Gumroad 已购：在内置页面登录、同步已购（`purchases.go`、`boothpane.go`、`booth_scraper.js`）、下载（`boothdl.go`）；Gumroad 读的是它的 Inertia 页面数据，已购和 Booth 的放在一起，编号带 `gr_` 前缀（`gumroad.go`） |
| `internal/pandl/` | 百度网盘登录（从内置页面取 Cookie）、转存到自己网盘（全部或勾选的部分）、断点续传下载 |
| `internal/ai/` | 流水线：AI 服务（`ai.go`：DeepSeek / 通义千问 / 智谱 GLM / Kimi / OpenAI 兼容 / Anthropic 兼容的对话和工具调用，设置和 Key 的保存）、给 AI 看图（`aivision.go`）、对话循环和给 AI 的工具（`aiagent.go`）、不用 AI 时的流程（`pipeline.go`）、工程里的素材列表（`projassets.go`） |
| `internal/server/` | 界面调用的接口（`api.go`、`pipeapi.go`）、反馈与建议（`feedback.go`） |
| `internal/testkit/`、`internal/unity/unitytest/` | 几个包的测试共用的小工具、测试用的假 Unity |
| `unityhelper/com.miovrc.pipeline` | Unity 这一侧（C#，只在编辑器里运行）：体检头像、把素体放进新场景、穿戴（Modular Avatar Setup Outfit）、生成菜单（MA Menu Item / Object Toggle / Material Setter；互斥组按参数分，独立开关用反转的 Object Toggle 做默认显示）、拍图标、给头像拍照。改动都走 Undo，不保存场景 |
| `unityhelper/com.miovrc.projectcard` | 工程封面插件 |
| `unityhelper/com.miovrc.studio` | 摄影棚插件（C#，只在编辑器里编译）：编辑器这一侧（`Editor/`）读软件留下的打开请求并进入 Play 模式；Play 模式里的摄影棚（`Runtime/`）用 IMGUI 画界面，拖骨骼摆姿势（IK）、手势、表情、视线、相机、灯光、背景和拍照。建的东西都不存进场景，退出时还原 |
| `web/` | 界面（HTML / CSS / JS，编译时嵌进 exe） |
| `packaging/` | 构建脚本（`build.sh`、`build.bat`）、安装包脚本和使用说明 |
| `test/` | 本地测试用的假 Booth / Gumroad / 网盘 / Google Drive / Dropbox / 翻译服务、假 AI 服务（`mockllm.py`）、假 Unity（`fakeunity.py`） |

包之间只从上往下引用，没有互相引用：`cmd/miovrca` → `server` → `ai`、`pandl` → `purchases` → `unity` → `library` → `booth` → `netdisk`、`translate`、`webpane`、`archive`、`update` → `naming` → `core`。下面的包需要通知上面的包时用回调变量（`webpane.PaneDownloadClicked`、`unity.AfterImport`），由上面的包在 `init` 里填上。

## 测试

```sh
go test ./...
```

界面和联网功能可以接到 `test/` 里的假服务上测试，不碰真实账号：

```sh
python3 test/mockbooth.py 47990
python3 test/mockweb.py 47991
VRCLIB_BOOTH_BASE=http://127.0.0.1:47990 VRCLIB_BOOTH_DL=http://127.0.0.1:47990 \
VRCLIB_PAN_BASE=http://127.0.0.1:47991 VRCLIB_BOOTH_WEB=http://127.0.0.1:47991 VRCLIB_BING_BASE=http://127.0.0.1:47991 \
VRCLIB_XY_BASE=http://127.0.0.1:47991/xy VRCLIB_GUMROAD_BASE=http://127.0.0.1:47991/gum \
VRCLIB_PCS_BASE=http://127.0.0.1:47991 VRCLIB_BAIDU_LOGIN=http://127.0.0.1:47991/mock/bdlogin \
VRCLIB_BROWSER=<chrome 路径> VRCLIB_HEADLESS=1 \
go run ./cmd/miovrca --no-window --data ./testdata
```

流水线可以接到假的 AI 服务、假的 Unity 和假的 VPM 仓库上测试：

```sh
python3 test/mockllm.py                 # 47992：OpenAI 兼容和 Anthropic 兼容，Key 是 sk-good-key-1234，模型 mio-large（看不了图）和 mio-vision（能看图）
python3 test/fakeunity.py <工程文件夹>   # 扮演工程里的流水线插件（FAKE_EMPTY=1 时场景里没有头像）
python3 test/fakevpm.py 47994           # 内置仓库的列表、zip 和工程模板；运行软件时设 VRCLIB_VPM_BASE=http://127.0.0.1:47994
```

其他店铺、愿望单和界面语言：

```sh
python3 test/mockjinxxy.py 47995        # 假的 Jinxxy；运行软件时设 VRCLIB_JINXXY_BASE=http://127.0.0.1:47995（mockweb 用 MOCK_JX 指过去）
VRCLIB_WISH_FAST=1                      # 愿望单的价格检查按秒进行（只用于测试）
python3 test/mockcloud.py 47993         # 假的 Google Drive 和 Dropbox；运行软件时设 VRCLIB_GDRIVE_BASE、VRCLIB_GDRIVE_API、VRCLIB_DROPBOX_BASE 为 http://127.0.0.1:47993
python3 test/i18n_harvest.py            # 列出两个词典（web/i18n-en.js、web/i18n-ja.js）还没覆盖的中文文字
python3 test/i18n_crawl.py --data <数据文件夹>   # 用英文和日文把每个界面走一遍，列出还留在屏幕上的中文
```

Google Drive 的分享默认按网页的方式读取，不需要 API Key；构建时设置 `cloudshare.GDriveAPIKey`（或运行时设 `VRCLIB_GDRIVE_KEY`）后改用 Drive API 列文件夹。

新增界面文字的做法：源码里照常写中文，再把这句话加进两个词典的 `exact`（整句）或 `patterns`（带数值的句子），跑一遍 `i18n_harvest.py` 确认没有遗漏。


### 流水线怎么和 Unity 传话

流水线插件每两秒写一次 `UserSettings/MioVRCA/bridge/alive.json`（Unity 开着、在不在编译、UnitySkills 的端口）。软件要它做事时写一个 `req_<编号>.json`（`{"id", "cmd", "args"}`）；插件接手时把它改名成 `run_<编号>.json`，在编辑器主线程里执行，写回 `res_<编号>.json`。命令有 `inspect`、`inspect_object`、`prefabs`、`snapshot`（给头像拍照，图片写到 `UserSettings/MioVRCA/shots`，软件读走后删掉）、`refresh`、`dress`、`build_menu`、`icons`、`place_avatar`、`undo`、`select`。两分钟没人处理的请求会被丢弃，不会在以后打开 Unity 时突然执行。这条通道不开网络端口，也不依赖 UnitySkills 的权限模式；UnitySkills 只用于玩家在对话里要求的其他操作。穿戴和生成菜单各是 Unity 撤销记录里的一步；计划执行到一半出错时整步收回。

调用 UnitySkills 的操作前，软件按它自己给每个操作标的信息（`/skills?full=1` 里的 `readOnly`、`mutatesAssets`、`riskLevel`、`operation` 等）决定要不要先问玩家：只读的、只改场景且能撤销的低风险操作直接执行，会删除、改工程文件、执行命令、重新编译或进 Play 模式的先问。

## 说明

- 本工具和 BOOTH、pixiv、闲鱼、百度网盘、VRChat 都没有关系。它读取的是这些网站的网页和公开接口，网站改版后部分功能可能失效。
- 本工具不会上传你的素材数据。它会连接：Booth、Gumroad（登录过才连）、百度网盘、必应翻译（谷歌翻译备用）、GitHub（检查更新），发送反馈时连接 FormSubmit，用流水线的 AI 时连接你填的 AI 服务接口地址；创建基础工程时连接 GitHub 和各插件的 VPM 仓库（packages.vrchat.com、vpm.nadena.dev、lilxyzw.github.io、vpm.anatawa12.com、vcc.vrcfury.com）（对话内容包括头像下的物体名、网格名、prefab 路径和菜单结构，不含贴图和模型文件；AI 拍照检查时，头像的截图会发给你选的 AI 服务或看图模型，在「看图」里选「不给 AI 看」就不发），以及设置里填写的代理；内置页面打开的网站（Booth、闲鱼等）由页面自己连接。
- 百度网盘下载用的是网盘网页和客户端的接口，不是开放平台接口；只用你自己账号的正常权限，不绕过限速，也不会删除你网盘里的文件（存进 `/MioVRCA` 的副本需要时自己删）。
- 流水线用 AI 时的费用由你选的 AI 服务商按用量收取，软件不经手。AI 做的改动都走 Unity 的撤销，软件不会保存场景；会删除东西、保存场景、执行脚本的 UnitySkills 操作要你点「允许」才执行。
- 内置页面开着时，会在 127.0.0.1 上开一个 DevTools 端口，软件靠它控制页面（后退、读取已购、下载按钮）。只有本机程序能连到它。

## 第三方代码

| 项目 | 许可证 |
| --- | --- |
| [jchv/go-webview2](https://github.com/jchv/go-webview2) | MIT（有少量修改，见 `third_party/go-webview2/VRCLIB_PATCHES.txt`） |
| [jchv/go-winloader](https://github.com/jchv/go-winloader) | ISC |
| [golang.org/x/sys](https://go.googlesource.com/sys) | BSD-3-Clause |
| WebView2Loader.dll（Microsoft WebView2 SDK） | 见 `third_party/go-webview2/webviewloader/sdk/LICENSE.txt` |
| [UnitySkills](https://github.com/Besty0728/Unity-Skills) 2.8.4（`unitykit/`，原样打包，去掉了 Tests） | MIT，见 `unitykit/UnitySkills-LICENSE.txt` |

Booth 已购页面的读取方式参考了 [BoothDownloader](https://github.com/Myrkie/BoothDownloader) 和 [booth-library-manager](https://github.com/yoshiki-0428/booth-library-manager)，百度网盘分享的读取方式参考了 [AList 的百度分享驱动](https://alistgo.com/guide/drivers/baidu.share.html)，穿戴和菜单靠玩家工程里的 [Modular Avatar](https://modular-avatar.nadena.dev/)（不随软件分发）。转存和下载参考了 [BaiduPCS-Go](https://github.com/qjfoidnh/BaiduPCS-Go) 和 [BaiduPCS-Py](https://github.com/PeterDing/BaiduPCS-Py)。

A local desktop asset manager for VRChat avatar modding. Organize Booth purchases, local folders and Baidu Netdisk links (share codes & paths) as clickable cards. | VRChat改模素材管理工具：Booth购买记录、本地文件夹、百度网盘链接一站式管理 | VRChat改変素材管理ツール：BOOTH購入品・フォルダ・ネットドライブリンクを一括管理MioVRCA
关键词：VRChat 改模 素材管理 换装 Booth 百度网盘 素材库MioVRCA
キーワード：VRChat アバター改変 素材管理 BOOTH 購入品管理 衣装MioVRCA
