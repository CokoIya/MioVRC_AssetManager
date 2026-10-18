# MioVRCA（MioVRC_AssetManager）

Windows 上的 VRChat 素材管理工具。扫描本地素材文件夹、Booth 已购和百度网盘分享，整理成可以浏览、筛选的素材库，标出哪些素材被 Unity 工程用到，并能一键导入工程。

单个 exe，有自己的窗口，数据只保存在本机。网站：<https://miovrc.com/vrca/>

作者：天川澪（VRC 同名）。QQ 交流群：<https://qm.qq.com/q/5d1fxbkYhy>　Discord：<https://discord.gg/CD8NVJwBA>

## 公开仓库说明

从 1.8.0 起，本仓库发布安装版、便携版和版本说明，不再发布完整的应用源码。本次没有上传 1.8.0 的应用源码，公开目录仅保留部分参考代码。

## 功能

- 扫描素材文件夹：识别解压后的文件夹、`.unitypackage` 和压缩包，判断分类（素体、衣服、头发、配饰……）和适配素体（文件名，加上 Booth 商品页的标题、标签和「対応アバター」段落）；素材旁边同名的图片当封面，散放着多个「包 + 同名图片」的文件夹按多个素材收录（同一素材的各配色、各素体版本除外）；素材里按配色、眼睛颜色分开的包或文件夹，以及只装图片、材质的部件文件夹，算作素材的一部分；「批量整理」可以一次给多个素材改分类、隐藏、不再收录
- 同款合并：同一商品的多个下载（不同素体的包、PSD 包）合成一张卡片，按素体筛选时只显示对应版本
- 衣服风格标签：按 Booth 标签自动分类（JK、Sexy、H、可爱、女仆、成熟……），可自定义
- PSD 源文件：列出 PSD / CLIP / SAI 等文件和尺寸
- 工程使用：分析 Unity 工程，标出素材在哪些工程里用到。工程文件夹下按模型分文件夹、再放多个版本的工程也能找到（向下 4 层，工程名是它在工程文件夹下的路径，如 `Manuka/v2_FT`）
- 工程页：每个 Unity 工程一张卡片（Unity 版本、上次打开、用到的素材），用对应版本的 Unity 一键打开；用不到的工程（备份、旧版本）可以隐藏，不再出现在工程页、流水线和导入的工程列表里；可选的封面插件（`com.miovrc.projectcard`，只在编辑器里运行）在打开工程、保存场景时给模型拍正面照当封面。工程里的插件管理交给 VCC / ALCOM
- 一键导入 Unity 工程：自动解压素材里的压缩包（PSD 包、分卷、嵌套、带密码），解压完压缩包移到回收站，再把 unitypackage 直接写进工程（按 GUID 就地更新已有资源，重名不覆盖，还没有 .meta 的文件也不覆盖；不同素体的包按工程的素体自动选）。一个素材里有多个包时：重复的只导一次，同一个包的多个版本只导最新版，PC 版和 Quest 版只导工程平台对应的那个（Android 工程导 Quest 版），会改写同一批文件的变体（左右手、带或不带某个功能）先列出来让你选；没导入的在结果里列出。导入素体到用着另一个素体的工程前会先确认；目标工程跟着流水线当前的工程走。分卷支持 7-Zip 式的 .001 编号和 ZIP 原生的 .z01 + .zip，缺卷时说出缺的是哪一卷；zip 和 unitypackage 的 .001 分卷软件自己合并。rar、7z 和其他分卷用电脑上默认的解压软件（7-Zip、Bandizip、WinRAR、好压、NanaZip、PeaZip）
- 流水线页：工程 → 素材 → 装配 → 菜单 → 验收。工程 Assets 里导入过的素材自动列出（带素材库里的名字和类别），勾上要装的，按类别装到模型上（衣服互斥切换 + 部件开关 + 换色；头发单独互斥，素体自带的头发可切回；配饰开关默认显示；道具开关默认隐藏；素体在场景没有模型时放进新场景），再按玩家定的层级生成 Modular Avatar 菜单和图标。给工程装上流水线插件（`com.miovrc.pipeline`）和 [UnitySkills](https://github.com/Besty0728/Unity-Skills) 后，还能让 AI 接着做别的改模操作。AI 服务由玩家自己选：DeepSeek、通义千问、智谱 GLM、Kimi、ChatGPT 兼容（OpenAI 接口）、Claude 兼容（Anthropic 接口），也可以登录「ChatGPT 账号」用自己的 ChatGPT 套餐（不用 API Key）；也可以不用 AI，按网格名字分组。AI 可以给头像拍照检查结果（穿模、错位、材质丢失），截图同时显示在验收记录里：模型能看图就直接把图发给它；看不了图的（DeepSeek）可以另设一个「看图模型」把画面描述给它；第一次用时自动测一次主模型能不能看图。「AI 用量与花费」按模型统计 tokens 和估算金额（DeepSeek 用官方价格，其他服务商自己填价格），DeepSeek、Kimi 的官方接口可以直接查余额。用 AI 时程序和 AI 分工：穿戴、插件、皮肤和菜单草案由程序完成，AI 只用一次请求整理衣服和头发的菜单（部件开关、名字、配色），程序校验它的方案后再生成（不合格让它改一次，还不行就用草案），最后请 AI 看一次截图和体检结果，一次运行 2～3 次请求；对话里 AI 原地打转（同一个失败操作第三次、连续 8 步失败、连续 4 次相同操作）时自己停下；看过的截图及时撤下、历史一次整理到位，让服务商的缓存多命中；一次运行默认最多 100 万 tokens，到了先问玩家要不要继续。评测：每次运行的菜单请求自动存成样本，可以用别的模型重新跑，比较通过率、用量和花费
- 一键创建基础工程：Unity 2022.3.22f1 的 VRChat 头像工程（工程设置取自 VRChat 官方模板），从各插件的官方 VPM 仓库解析并下载最新稳定版（VRChat SDK、Modular Avatar、lilToon、Avatar Optimizer、Gesture Manager、VRCFury，可勾选；复用 ALCOM / VCC 的缓存；UnitySkills 默认勾选，本机 Git 能连上 GitHub 时按它的官方链接装，否则装随软件带的版本），可顺带导入素材库里的素体、装 Unity 插件并打开 Unity；新工程登记进 ALCOM / VCC 的工程列表。检测本机的 ALCOM、VCC、Unity Hub 和所需 Unity 版本
- Booth 商品：按文件夹名里的编号、Booth 链接、已购记录或名称搜索关联，显示图片、标签和说明（可翻译）。左侧「Booth 关联」筛出未关联、待确认的素材，在详情里一个接一个关联；关联会同步到同一商品的其他版本
- 封面：自己选的图片、Booth 商品图、素材旁边的图片、在 Unity 里给 prefab 拍的照片，都没有时用 unitypackage 里 Unity 生成的预览图（最大 128 像素，按原尺寸显示，卡片上标「预览图」并提示怎样换成清晰的）
- 愿望单：Booth 商品每天检查一次价格，降价提醒；列表或网格显示；勾选「同步 Booth 上的スキ」后，同步已购时把在 Booth 点了「スキ！」的商品加入
- Booth 页：点选分类、素体、风格标签逛 Booth（可多选），标出已购和素材库已有的，可以隐藏它们
- 内置页面：Booth 的商品页、登录、购物车、已购列表在软件里打开（第二个 WebView2，登录保存在本机），可以直接购买
- 同步 Booth 已购：在内置页面登录 Booth，读取已购、礼物和订单
- 下载 Booth 已购：在软件里直接下载（也包括在内置 Booth 页面里点的下载），自动解压到素材文件夹并入库；可以「下载并导入」
- Gumroad 已购：在内置页面登录 Gumroad，读取已购（含已归档），和 Booth 已购一样显示成卡片、下载、解压、入库
- 闲鱼：在软件内一个独立的内嵌浏览器里搜索、聊天、下单（软件不控制这个浏览器，也不保存它的登录），或者交给默认浏览器；复制卖家发来的网盘分享后可以一键收进素材库
- 百度网盘：读取分享里的文件列表，只在网盘里的素材也能收录；合集分享按分类文件夹拆成多张卡片
- 百度网盘下载：在内置页面登录百度网盘（登录加密保存在本机，可以更换账号、退出），分享先存进自己网盘的 `/MioVRCA`，再下载、解压、入库；可以只勾选分享里的部分文件或文件夹；断了可以接着下；百度要求验证码时在软件里显示验证码图片，输入后接着下；也可以在百度网盘客户端里把分享保存到自己的网盘，再在软件里选网盘中的文件夹下载入库。速度取决于账号的会员等级（只有超级会员不限速）；超级会员下载 32 MB 以上的文件时同时开 6 个连接分段下载，百度拒绝时退回一个连接
- 合集包：网盘分享是一个总压缩包、里面是多个素材的压缩包（或分类压缩包）时，下载解压后每个素材各建一张卡片（同一素材的多素体版本包、PSD 包、按配色或眼睛颜色分开的包不拆）；没拆开的合集（没自动解压的、以前下载的、自己放进素材文件夹的）卡片上标「合集 · N」，不能一键导入，可以「拆分为 N 个素材」或设成「作为单个素材」；一键导入时解压后才发现是合集的（rar、7z，或里面的压缩包只按分类、编号命名），改成拆分，不导入
- 复制链接提示：在别的程序里复制闲鱼、Booth 或网盘分享链接后切回软件，任意页面顶部提示「打开」或「收录」（设置里可以关掉）
- 下载位置：默认是 C 盘以外剩余空间最大的盘的 `MioVRCdownload`（优先素材文件夹所在的盘），设置里可以改
- 新素材自动整理：软件开着时，素材文件夹里新放入的素材会自动扫描归类
- 更新提示：网盘分享每天、Booth 商品页每周检查一次变化
- 搜索：名称、店铺、标签、路径、备注里找；空格分开的词要同时包含，词前加 `/` 是「包含其一即可」，加 `-` 是「不包含」，可以混用（`贴图 /发型 /服装 -眼睛`），带空格的词组用引号括起来；搜索栏下方显示软件是怎么理解这次搜索的
- 中文名：素材名下面显示中文翻译，可以手动改
- 检查更新：从本仓库的 Releases 下载并替换，更新后显示这一版的更新内容
- 托盘和提示音：点窗口的 × 收到托盘，导入、下载、流水线继续进行（设置里可以改成直接退出）；流水线或 AI 运行结束、需要你确认或选择、新建工程和「下载并更新到工程」完成时响提示音，窗口不在前面时任务栏闪烁，收在托盘里时弹出通知
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

流水线放进 Unity 工程的东西（点「移除」会删掉）：`Packages/com.miovrc.pipeline`、`Packages/com.besty.unity-skills`（工程自己已经有 UnitySkills 时不放也不删；新建工程时按官方链接装的只是 `Packages/manifest.json` 里的一行，由 Unity 从 GitHub 下载）、`UserSettings/MioVRCA/bridge`（软件和 Unity 之间传话用的文件）、`UserSettings/MioVRCA/shots`（拍照时的临时图片，软件读走就删）。生成的菜单图标在工程的 `Assets/MioVRCA/<头像名>/Icons`。

## 从源码构建（历史说明）

需要 Go 1.24 或更新版本。第三方库已经放在 `third_party/` 里，构建时不需要联网下载依赖。

**在 Windows 上**：双击 `packaging\build.bat`，或者在仓库根目录执行：

```bat
go test ./...
go build -trimpath -ldflags "-H windowsgui -s -w" -o dist\MioVRC_AssetManager.exe ./cmd/miovrca
```

**在 Linux / WSL 上交叉编译**，同时生成安装包和便携版（需要 NSIS 3）：

```sh
sh packaging/build.sh
```

`cmd/miovrca/rsrc_windows_amd64.syso` 里是图标、版本信息和高 DPI 清单，已经生成好了。改了同一个文件夹里的 `app.rc`、`app.ico` 或 `app.manifest` 之后，需要重新生成：

```sh
cd cmd/miovrca
x86_64-w64-mingw32-windres --preprocessor=cat -c 65001 -O coff -i app.rc -o rsrc_windows_amd64.syso
```

## 发布新版本（历史说明）

软件从本仓库的 Releases 检查更新（只看最新的正式版）：

1. 改版本号：`internal/core/version.go` 的 `AppVersion`、`cmd/miovrca/app.rc`、`cmd/miovrca/app.manifest`、`packaging/installer.nsi`，重新生成 `rsrc_windows_amd64.syso`（命令见上）。
2. 在 `CHANGELOG.md` 最上面写这一版的更新内容。它会编译进程序，更新后第一次打开时显示，也显示在「更新日志」里。不需要告诉玩家的版本可以不写新条目（1.7.4 就沿用 1.7.3 的）：从 1.7.3 升上来的不再弹出，从更早版本升上来的看到的是 1.7.3 的条目。
3. `sh packaging/build.sh`，在 `dist/` 里得到 `MioVRC_AssetManager-setup-x.y.z.exe` 和 `MioVRC_AssetManager-portable-x.y.z.zip`。
4. 新建 Release，标签写成 `v1.6.0` 这样的格式，上传这两个文件。Release 的说明会显示在软件的更新窗口里。
   - 软件下载便携版 zip，取出 exe 替换自己后重启；只有安装版时会下载并打开安装程序。
   - 附件名用英文，中文名在 GitHub 上可能被改掉。

## 说明

- 本工具和 BOOTH、pixiv、闲鱼、百度网盘、VRChat 都没有关系。它读取的是这些网站的网页和公开接口，网站改版后部分功能可能失效。
- 本工具不会上传你的素材数据。它会连接：Booth、Gumroad（登录过才连）、百度网盘、必应翻译（谷歌翻译备用）、GitHub（检查更新），发送反馈时连接 FormSubmit，用流水线的 AI 时连接你填的 AI 服务接口地址；创建基础工程时连接 GitHub 和各插件的 VPM 仓库（packages.vrchat.com、vpm.nadena.dev、lilxyzw.github.io、vpm.anatawa12.com、vcc.vrcfury.com）（对话内容包括头像下的物体名、网格名、prefab 路径和菜单结构，不含贴图和模型文件；AI 拍照检查时，头像的截图会发给你选的 AI 服务或看图模型，在「看图」里选「不给 AI 看」就不发），以及设置里填写的代理；内置页面打开的网站（Booth、闲鱼等）由页面自己连接。
- 百度网盘下载用的是网盘网页和客户端的接口，不是开放平台接口；只用你自己账号的正常权限，不绕过限速（只有超级会员账号分段多连接下载，和官方客户端一样；其他账号一个连接），不自动识别验证码（由你自己输入），也不会删除你网盘里的文件（存进 `/MioVRCA` 的副本需要时自己删）。
- 流水线用 AI 时的费用由你选的 AI 服务商按用量收取，软件不经手。AI 做的改动都走 Unity 的撤销，软件不会保存场景；会删除东西、保存场景、执行脚本的 UnitySkills 操作要你点「允许」才执行。
- 内置页面开着时，会在 127.0.0.1 上开一个 DevTools 端口，软件靠它控制页面（后退、读取已购、下载按钮）。只有本机程序能连到它。闲鱼、淘宝、支付宝的页面不在这个浏览器里打开：软件不会把这些地址交给它，它的页面自己跳转过去或弹出这些网站的窗口时，会退回原页面（或关闭窗口）并改用默认浏览器。这个判断发生在页面开始加载之后，所以内置浏览器可能已经向这些网站发出过一次不带登录的请求。
- 闲鱼页面在独立的内嵌浏览器里打开时，软件不开调试端口、不向页面注入或执行脚本、不读写 Cookie、不使用代理设置，也不接管页面打开的新窗口，只从外部取得当前地址、标题和能否后退。它仍可能被网站认出是应用内的浏览器（WebView2 自身带有可识别的特征），软件不能保证不触发平台的风控。
- 软件在前台时，会查看剪贴板里有没有闲鱼、Booth 或网盘分享链接（用于顶部的「打开 / 收录」提示和「收录网盘链接」），只认这三类：网盘分享把复制的那段文字交给界面，闲鱼和 Booth 只交出链接本身，别的内容不传给界面、不保存、不记录；被来源程序标记为不供剪贴板监视程序读取的内容（如密码管理器复制的密码）不读取。可以在设置的「自动化」或「闲鱼」页左侧关闭。

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
