# MioVRCA（MioVRC_AssetManager）

> 本公开仓库仅保留 `web/` 中的 UI 源码、许可文本和配套说明，不包含完整应用源码。完整软件请从 Releases 下载；以下功能介绍面向已发布的软件。

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

摄影棚放进 Unity 工程的东西（工程详情里点「移除摄影棚」会删掉；摄影棚开着时不能移除，要先退出 Play 模式）：`Packages/com.miovrc.studio`、`UserSettings/MioVRCA/studio`（摄影棚的设置、保存的姿势和表情；摄影棚开着时每 5 秒刷新一次的 `running`，软件看它知道摄影棚开着；软件请 Unity 打开摄影棚时留下的 `open.json`，Unity 读到就删）。照片不在工程里，在「图片」文件夹的 `MioVRCA/<工程名>`，移除时不删。

## UI 源码与公开范围

本仓库公开 `web/` 下的 HTML、CSS、JavaScript 和多语言界面文件。UI 文件需要完整应用提供的本地服务，不能仅凭本仓库构建或运行完整的 MioVRCA。

后端业务逻辑、AI 流水线、素材识别、下载与已购读取、Unity 插件及导入逻辑、测试、安装器和构建脚本均不在当前公开范围内。完整应用的安装版和便携版继续通过 [Releases](https://github.com/CokoIya/MioVRC_AssetManager/releases) 提供。

### 历史源码获取声明

**在适用法律、GitHub 服务条款或你已依法取得的许可证未授予相应权利的范围内，未经作者另行书面授权，禁止通过历史提交、旧标签、旧版源码归档、缓存或镜像获取、恢复、复制或再分发已撤回公开的非 UI 源码。**

本声明不追溯撤销既有许可，不限制既有许可证或法律已授予的权利，也不是 GPL 的额外限制；如有冲突，以适用的许可证及法律为准。完整条款见 [LICENSE.md](LICENSE.md)。本次调整当前分支的公开范围，不代表历史提交、缓存或他人副本已被技术清除。

## 说明

- 本工具和 BOOTH、pixiv、闲鱼、百度网盘、VRChat 都没有关系。它读取的是这些网站的网页和公开接口，网站改版后部分功能可能失效。
- 本工具不会上传你的素材数据。它会连接：Booth、Gumroad（登录过才连）、百度网盘、必应翻译（谷歌翻译备用）、GitHub（检查更新），发送反馈时连接 FormSubmit，用流水线的 AI 时连接你填的 AI 服务接口地址；创建基础工程时连接 GitHub 和各插件的 VPM 仓库（packages.vrchat.com、vpm.nadena.dev、lilxyzw.github.io、vpm.anatawa12.com、vcc.vrcfury.com）（对话内容包括头像下的物体名、网格名、prefab 路径和菜单结构，不含贴图和模型文件；AI 拍照检查时，头像的截图会发给你选的 AI 服务或看图模型，在「看图」里选「不给 AI 看」就不发），以及设置里填写的代理；内置页面打开的网站（Booth、闲鱼等）由页面自己连接。
- 百度网盘下载用的是网盘网页和客户端的接口，不是开放平台接口；只用你自己账号的正常权限，不绕过限速，也不会删除你网盘里的文件（存进 `/MioVRCA` 的副本需要时自己删）。
- 流水线用 AI 时的费用由你选的 AI 服务商按用量收取，软件不经手。AI 做的改动都走 Unity 的撤销，软件不会保存场景；会删除东西、保存场景、执行脚本的 UnitySkills 操作要你点「允许」才执行。
- 内置页面开着时，会在 127.0.0.1 上开一个 DevTools 端口，软件靠它控制页面（后退、读取已购、下载按钮）。只有本机程序能连到它。

## 许可证

本仓库当前公开的作者自有 UI 源码及配套说明文件以 **GPL-3.0** 或 **PolyForm Noncommercial 1.0.0** 授权，任选其一，详见 [LICENSE.md](LICENSE.md)。本说明不对未公开的非 UI 源码作新的授权：

- 个人使用、学习、修改都可以；不收费地分享原版或修改版也可以，按所选许可证的条件来。
- 商业使用（出售、收费分发、用在收费的产品或服务里）只能按 GPL-3.0，并且必须以 GPL-3.0 公开全部源码。不公开源码的商业使用，两种许可证都不允许。
- 按 GPL-3.0 发布修改版时，界面里要保留「基于 MioVRCA（作者：天川澪）」和本项目的地址，标明是修改版，并换用自己的名称和图标（GPL-3.0 第 7 条的附加条款）。
- 「MioVRCA」「MioVRC_AssetManager」这两个名称和软件图标不在授权范围内。第三方代码按各自的许可证，见下面的「第三方代码」。

本仓库仅接收公开 UI 范围内的贡献，请勿提交已撤回的非 UI 源码。

## 第三方代码

当前公开仓库不再包含第三方组件的实现代码、DLL 或 UnitySkills 源码压缩包。以下记录用于保留已发布软件的组件来源和许可信息；现有第三方许可文本仍予保留，且不受上述历史源码声明限制。

| 项目 | 许可证 |
| --- | --- |
| [jchv/go-webview2](https://github.com/jchv/go-webview2) | [MIT](third_party/go-webview2/LICENSE) |
| [jchv/go-winloader](https://github.com/jchv/go-winloader) | [ISC](third_party/go-winloader/LICENSE.md) |
| [golang.org/x/sys](https://go.googlesource.com/sys) | [BSD-3-Clause](third_party/sys/LICENSE)，另见 [PATENTS](third_party/sys/PATENTS) |
| [ledongthuc/pdf](https://github.com/ledongthuc/pdf)（读取 PDF 说明书的文字） | BSD-3-Clause，见上游项目 |
| WebView2Loader.dll（Microsoft WebView2 SDK） | [SDK 许可](third_party/go-webview2/webviewloader/sdk/LICENSE.txt) |
| [UnitySkills](https://github.com/Besty0728/Unity-Skills) 2.8.4 | [MIT](unitykit/UnitySkills-LICENSE.txt) |

Booth 已购页面的读取方式参考了 [BoothDownloader](https://github.com/Myrkie/BoothDownloader) 和 [booth-library-manager](https://github.com/yoshiki-0428/booth-library-manager)，百度网盘分享的读取方式参考了 [AList 的百度分享驱动](https://alistgo.com/guide/drivers/baidu.share.html)，穿戴和菜单靠玩家工程里的 [Modular Avatar](https://modular-avatar.nadena.dev/)（不随软件分发）。转存和下载参考了 [BaiduPCS-Go](https://github.com/qjfoidnh/BaiduPCS-Go) 和 [BaiduPCS-Py](https://github.com/PeterDing/BaiduPCS-Py)。

A local desktop asset manager for VRChat avatar modding. Organize Booth purchases, local folders and Baidu Netdisk links (share codes & paths) as clickable cards. | VRChat改模素材管理工具：Booth购买记录、本地文件夹、百度网盘链接一站式管理 | VRChat改変素材管理ツール：BOOTH購入品・フォルダ・ネットドライブリンクを一括管理MioVRCA
关键词：VRChat 改模 素材管理 换装 Booth 百度网盘 素材库MioVRCA
キーワード：VRChat アバター改変 素材管理 BOOTH 購入品管理 衣装MioVRCA
