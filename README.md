# MioVRCA（MioVRC_AssetManager）

Windows 上的 VRChat 改模素材管理工具。把本地文件夹、Booth / Gumroad 已购和网盘分享里的素材整理成一个素材库，一键导入 Unity 工程，再用自动化流水线装配到模型上。

[官网](https://miovrc.com/vrca/) ・ [下载](https://github.com/CokoIya/MioVRC_AssetManager/releases) ・ [QQ 交流群 1077205718](https://qm.qq.com/q/9wErKBClA4) ・ [Discord](https://discord.gg/CD8NVJwBA) ・ 作者：天川澪（VRC 同名）

> 本公开仓库仅保留 `web/` 中的 UI 源码、许可文本和配套说明，不包含完整应用源码。完整软件请从 Releases 下载；以下功能介绍面向已发布的软件。

## 功能

- **2.0.0 一键改模**：把素体和素材加入「改模篮」，查看兼容性、同步参数和磁盘空间预估，再一键新建或选择工程、导入、穿戴、生成菜单并验收，结果页可继续上传到 VRChat、创建 Quest 版或撤销本次改动。
- **素材库**：扫描素材文件夹、unitypackage 和压缩包，自动分类、识别适配素体、关联 Booth 商品；同款合并、风格标签、搜索筛选；标出每个素材用在哪些 Unity 工程里。
- **购买和下载**：在软件里逛 Booth，同步并下载 Booth、Gumroad 已购；读取并下载百度网盘、Google Drive、Dropbox 分享；闲鱼在独立窗口中打开；愿望单降价提醒。
- **工程**：一键导入素材（自动解压，按素体选包，跳过重复）；一键新建 VRChat 基础工程；上传前体检和一键处理。
- **自动化流水线**：把衣服、头发、配饰、道具装配到模型上，按层级生成菜单和图标，可以接 DeepSeek、通义千问、ChatGPT、Claude 等 AI 继续改模。还能自动验收和修复穿模、一句话调外观、按说明书操作，并提供 3D 试衣间、照图配装、工程时光机和一键 Quest 版。
- **宣传视频**：AI 担任导演，自动收集 Unity 拍摄的镜头、素材封面和 Booth 商品图，排版、按音乐节拍剪辑，导出 MP4，并写好各平台的标题和简介。
- **社区（2.0.1）**：提供[网页版 miovrc.net](https://miovrc.net)，手机、电脑浏览器与软件共用账号；支持邮箱验证码、Google、Discord 和扫码登录，可发帖、分享作品和装配方案、关注、回复、转帖、点赞和私信。新增帖子翻译、精选、Booth 情报和推荐筛选；手机可添加到主屏幕并接收推送通知。无需 GitHub 账号，详见[隐私政策](https://miovrc.net/privacy.html)。
- **外部 AI 接入**：通过 MCP 让 Claude Code、Codex 等 AI 工具使用 MioVRCA；配合 MioVRCT，可以不摘头显改模。
- 界面支持中文、English、日本語。

## 安装

在 [Releases](https://github.com/CokoIya/MioVRC_AssetManager/releases) 下载安装版 `MioVRC_AssetManager-setup-x.y.z.exe` 或便携版 `MioVRC_AssetManager-portable-x.y.z.zip`。需要 Windows 10 / 11（64 位）。软件会自动检查更新。

## UI 源码与公开范围

本仓库公开 `web/` 下的 HTML、CSS、JavaScript 和多语言界面文件。UI 文件需要完整应用提供的本地服务，不能仅凭本仓库构建或运行完整的 MioVRCA。

后端业务逻辑、AI 流水线、素材识别、下载与已购读取、Unity 插件及导入逻辑、测试、安装器和构建脚本均不在当前公开范围内。完整应用的安装版和便携版继续通过 [Releases](https://github.com/CokoIya/MioVRC_AssetManager/releases) 提供。

### 历史源码获取声明

**在适用法律、GitHub 服务条款或你已依法取得的许可证未授予相应权利的范围内，未经作者另行书面授权，禁止通过历史提交、旧标签、旧版源码归档、缓存或镜像获取、恢复、复制或再分发已撤回公开的非 UI 源码。**

本声明不追溯撤销既有许可，不限制既有许可证或法律已授予的权利，也不是 GPL 的额外限制；如有冲突，以适用的许可证及法律为准。完整条款见 [LICENSE.md](LICENSE.md)。本次调整当前主分支的公开范围，不代表历史提交、缓存或他人副本已被技术清除。

## 说明

- 本工具与 VRChat、BOOTH、pixiv、闲鱼、百度网盘均无关联，读取的是这些网站的网页和公开接口，网站改版后部分功能可能失效。
- 素材库数据只保存在本机，登录信息加密保存。
- 网盘下载只使用你自己账号的正常权限，不绕过限速，验证码由你自己输入。

## 许可证

本仓库当前公开的作者自有 UI 源码及配套说明文件以 **GPL-3.0** 或 **PolyForm Noncommercial 1.0.0** 授权，任选其一，详见 [LICENSE.md](LICENSE.md)。本说明不对未公开的非 UI 源码作新的授权：

- 个人使用、学习、修改都可以；不收费地分享原版或修改版也可以，按所选许可证的条件来。
- 商业使用（出售、收费分发、用在收费的产品或服务里）只能按 GPL-3.0，并且必须以 GPL-3.0 公开全部源码。不公开源码的商业使用，两种许可证都不允许。
- 按 GPL-3.0 发布修改版时，界面里要保留「基于 MioVRCA（作者：天川澪）」和本项目的地址，标明是修改版，并换用自己的名称和图标（GPL-3.0 第 7 条的附加条款）。
- 「MioVRCA」「MioVRC_AssetManager」这两个名称和软件图标不在授权范围内。第三方代码按各自的许可证，见下面的「第三方代码」。

本仓库仅接收公开 UI 范围内的贡献，请勿提交已撤回的非 UI 源码。

## 第三方代码

当前公开仓库不包含非 UI 第三方组件的实现代码、DLL 或 UnitySkills 源码压缩包。界面所需的浏览器端第三方依赖及其许可保留在 `web/vendor/`，详见 [组件来源说明](web/vendor/README.txt)。以下记录保留已发布软件的组件来源和许可信息；第三方许可不受上述历史源码声明限制。

| 项目 | 许可证 |
| --- | --- |
| [jchv/go-webview2](https://github.com/jchv/go-webview2) | [MIT](third_party/go-webview2/LICENSE) |
| [jchv/go-winloader](https://github.com/jchv/go-winloader) | [ISC](third_party/go-winloader/LICENSE.md) |
| [golang.org/x/sys](https://go.googlesource.com/sys) | [BSD-3-Clause](third_party/sys/LICENSE)，另见 [PATENTS](third_party/sys/PATENTS) |
| [ledongthuc/pdf](https://github.com/ledongthuc/pdf)（读取 PDF 说明书的文字） | [BSD-3-Clause](third_party/pdf/LICENSE) |
| WebView2Loader.dll（Microsoft WebView2 SDK） | [SDK 许可](third_party/go-webview2/webviewloader/sdk/LICENSE.txt) |
| [UnitySkills](https://github.com/Besty0728/Unity-Skills) 2.8.4 | [MIT](unitykit/UnitySkills-LICENSE.txt) |
| [mp4-muxer](https://github.com/Vanilagy/mp4-muxer) 5.2.2 | [MIT](web/vendor/LICENSE-mp4-muxer.txt) |
| [webm-muxer](https://github.com/Vanilagy/webm-muxer) 5.1.4 | [MIT](web/vendor/LICENSE-webm-muxer.txt) |
| [qrcode-generator](https://github.com/kazuhikoarase/qrcode-generator) 2.0.4 | [MIT](web/vendor/LICENSE-qrcode-generator.txt) |
| [TensorFlow.js](https://github.com/tensorflow/tfjs) 4.22.0 | [Apache-2.0](web/vendor/LICENSE-tfjs.txt) |
| [NSFWJS](https://github.com/infinitered/nsfwjs) 4.4.0 MobileNetV2 模型 | [MIT](web/vendor/LICENSE-nsfwjs.txt) |

Booth 已购页面的读取方式参考了 [BoothDownloader](https://github.com/Myrkie/BoothDownloader) 和 [booth-library-manager](https://github.com/yoshiki-0428/booth-library-manager)，百度网盘分享的读取方式参考了 [AList 的百度分享驱动](https://alistgo.com/guide/drivers/baidu.share.html)，穿戴和菜单靠玩家工程里的 [Modular Avatar](https://modular-avatar.nadena.dev/)（不随软件分发）。转存和下载参考了 [BaiduPCS-Go](https://github.com/qjfoidnh/BaiduPCS-Go) 和 [BaiduPCS-Py](https://github.com/PeterDing/BaiduPCS-Py)。

A local desktop asset manager for VRChat avatar modding. Organize Booth purchases, local folders and Baidu Netdisk links (share codes & paths) as clickable cards. | VRChat改模素材管理工具：Booth购买记录、本地文件夹、百度网盘链接一站式管理 | VRChat改変素材管理ツール：BOOTH購入品・フォルダ・ネットドライブリンクを一括管理MioVRCA
关键词：VRChat 改模 素材管理 换装 Booth 百度网盘 素材库MioVRCA
キーワード：VRChat アバター改変 素材管理 BOOTH 購入品管理 衣装MioVRCA
