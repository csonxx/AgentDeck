# 0.3.7 界面精修 / Interface refinement

参考用户指定的[极氪汽车 App 官方截图](https://apps.apple.com/cn/app/%E6%9E%81%E6%B0%AA%E6%B1%BD%E8%BD%A6/id1570277888)，采用浅色页面、清晰的黑色字体、白色分组面板、细分隔线和小面积橙色强调。用 Flutter 原生组件实现，保留 AgentDeck 的信息结构；没有使用极氪品牌标识、汽车图片或商店素材。

The owner's reference is the [official ZEEKR app gallery](https://apps.apple.com/cn/app/%E6%9E%81%E6%B0%AA%E6%B1%BD%E8%BD%A6/id1570277888). Light surfaces, clear dark typography, white grouped panels, thin separators and limited orange accents inform a native Flutter interface that preserves AgentDeck's information structure. ZEEKR logos, car images and store assets are not shipped.

| 区域 / Area | 精修 / Refinement |
| --- | --- |
| 全局 / Foundation | 统一颜色、字体层级、圆角、控件尺寸、弹窗、搜索框、底部导航和反馈；中文标题沿用平台字体。 / Consistent colors, type hierarchy, corners, controls, dialogs, search, navigation and feedback; titles retain platform fonts. |
| 首页 / Home | 工作空间标题、连接状态点、Codex/Claude 切换、项目/最新会话的橙色指示线；真实项目与会话数量。 / Workspace heading, connection dot, agent switch, orange view indicator, actual project/chat counts. |
| 项目 / Projects | 白色文件夹面板、分隔线、计数、独立新建入口、最近更新时间、两行长标题；展开/收起箭头短动画。 / White folder panels, separators, counts, creation actions, update timestamps, two-line titles and a short expand/collapse animation. |
| 最新会话 / Recent | 小尺寸 agent 图标、项目来源、标题和更新时间，当前选择以淡橙色标记。 / Compact agent glyphs, project context, titles, update times and a soft orange selection. |
| 会话 / Conversation | 白色阅读区、统一 agent 图标、正文标题与引用样式、细边框的思考/工具折叠块、圆角图片、柔和用户气泡和独立输入面板。 / White reading canvas, unified agent glyphs, structured Markdown, outlined collapsible reasoning/tools, rounded images, soft user bubbles and a distinct composer. |
| 工作站 / Workstation | 深色身份卡片、连接状态、配对和忘记操作；加密说明保持可读。 / Dark identity surface, connection state, pairing/forget actions and readable encryption details. |
| 目录与弹窗 / Directory and dialogs | 统一搜索框、行距、底部弹层与操作按钮。 / Consistent search, rows, bottom sheets and actions. |

保留已有的按 agent 分组、项目顺序和显示隐藏、发送与停止、对话目录、历史分页、图片和链接行为。没有增加持续背景动画、图片资源包或 UI 库；列表仍按需构建，Markdown 子树复用机制保留。执行中的转圈继续只表达真实任务状态。

Existing agent grouping, project order/visibility, send/stop, directory, paged history, images and links remain available. No continuous decorative animation, image bundle or UI library is added. Lists retain lazy construction and unchanged Markdown subtrees remain reusable. Progress indicators continue to reflect actual running tasks.

视觉检查使用测试会话生成的 `artifacts/home.png` 和 `artifacts/conversation.png`；它们是合成测试画面，不是用户的真实会话，也不代表原生、公网或真实飞书验收。原生验证单独记录在对应版本验收文件中。

Visual inspection uses synthetic test-session renders in `artifacts/home.png` and `artifacts/conversation.png`. They contain test content, not the owner's conversations, and do not establish native, public-domain or real Feishu acceptance. Native verification is recorded separately in versioned acceptance receipts.


## 0.3.8 · 尺度、品牌与切换 / Scale, brand and selection

- 工作空间/工作站从 28–30px 收到 22px，配对/网关主标题从 34px 收到 24px，项目与正文采用 14px；正文标题采用 19/17/15px。保留系统字号放大、触摸目标及历史按需构建。 / Workspace/workstation headings shrink from 28–30px to 22px, pairing/gateway headings from 34px to 24px, and project/body text uses 14px with 19/17/15px Markdown headings. System text scaling, touch targets and lazy history remain supported.
- “项目 / 最新会话”改为灰色底上的白色分段选择，180ms 淡入淡出，移除标签上的水波纹、橙色短线与切换时的字重跳变；保留屏幕阅读器选中状态、键盘操作及焦点边框。 / Projects/Recent use white selection surfaces on a gray track with a 180ms fade, replacing label ripples, orange underline and switching font-weight changes. Selected semantics, keyboard activation and focus outlines remain available.
- App 名称统一为 AgentDeck，首页使用紧凑混合大小写字标。新图标将叠层卡片和终端光标结合，深色底、白色卡片及橙色光标；原始图形不是现有品牌素材。 / The display name is consistently AgentDeck, with a compact mixed-case wordmark. Original artwork combines layered decks and a terminal cursor, using a dark surface, white cards and orange cursor.
- [SVG 源图](brand/agentdeck-mark.svg) 与 scripts/generate_brand.py 可复现 Flutter、Android 分辨率与自适应图标、iOS 的 19 个无透明背景图标尺寸。图标生成仅是开发步骤，不增加运行时图形库。 / The [source SVG](brand/agentdeck-mark.svg) and scripts/generate_brand.py reproduce Flutter artwork, Android density/adaptive assets and 19 opaque iOS variants. Asset generation is a development step with no added runtime drawing library.
- home-pressed.png 与 home-recent.png 由已有合成 UI 测试检查按下和选中外观；它们仍不是用户的实际会话截图。 / Existing synthetic UI checks capture pressed/selected surfaces in home-pressed.png and home-recent.png; they are not the owner's actual chats.
