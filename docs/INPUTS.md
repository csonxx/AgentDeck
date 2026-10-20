# 会话输入 / Conversation input

## 模型与 effort / Model and effort

会话输入框左下角「模型 · Effort」读取工作站当前 CLI 的模型目录。选模型后只展示该模型支持的 effort；点击「应用到下次发送」保存本会话的选择，下一条消息携带参数。正在执行的回合使用原参数。强度选项与应用按钮固定在模型列表外，长目录仍可直接操作。列表来自 CLI，不表示账号已获得每个模型的调用权限；若服务商拒绝，显示真实错误。

The composer’s Model · Effort control reads the installed workstation CLI’s catalog. It offers only the selected model’s effort levels. Apply saves a per-chat selection for the next send; an active turn retains its current parameters. Reasoning choices and Apply remain visible outside the scrolling catalog. Catalog presence is not proof of account entitlement. Provider rejection remains visible.

Codex 使用 `model/list` 与 `turn/start.model/effort`。已关联桌面原会话时，通过原 owner 的 IPC 请求传入相同参数和图片，不替换原审批或 sandbox 策略。Claude 初始化获取模型别名及支持等级；改模型或 effort 时，空闲 CLI 进程以同一 session ID、`--resume`、`--model` 和 `--effort` 恢复，保留历史与默认权限。执行中拒绝改动并发发送。选择「沿用当前会话设置」沿用后端已生效的设置，不重置已有线程。

Codex uses `model/list` and `turn/start.model/effort`. Desktop-linked chats pass options and images to the original owner’s IPC, retaining its approval and sandbox policies. Claude advertises aliases/effort levels during initialization. Changing either option resumes an idle process with the same session ID and `--resume`, `--model`, `--effort`, retaining history and default permissions. Busy turns reject concurrent sends. Inherit means the backend’s current effective settings, rather than resetting a thread.

## 各 Agent 输入能力 / Agent input capabilities

| Agent | 模型与强度 / Model and reasoning | 图片 / Images | 普通文件 / General files | 实时语音 / Live voice |
| --- | --- | --- | --- | --- |
| Codex | 实际 model/list 与 Effort / Native catalog and effort | 原生图片参数 / Native image input | 加密附件只读引用 / Encrypted owner-only reference | 手机离线流式 / Phone offline streaming |
| Claude Code | 实际 CLI 模型与 Effort / CLI models and effort | 原生 base64 block / Native base64 block | 同上 / Same | 同上 / Same |
| OpenCode | 已连接 provider 的实际模型与 Variant / Connected providers' models and variants | 原生 file part，需模型支持 image / Native file part requiring image-capable model | 同上 / Same | 同上 / Same |
| Qoder、TRAE | ACP configOptions 的 model、thought_level；未返回时显示原因 / Advertised ACP config options, with explicit unavailability | 根据 ACP promptCapabilities.image / Negotiated ACP image support | 同上 / Same | 同上 / Same |
| WorkBuddy | 当前本地助理接口未提供 / Unavailable in current Local Assistant API | 当前接口未提供 / Unavailable in current API | 作为文字中的本机只读引用 / Owner-only reference in text | 同上 / Same |

0.4.5 语音在手机识别，不检查 Local 转写服务或模型目录。入口位于已关联的会话中。普通文件读取属于 agent 的工具操作，仍可能要求你批准；不会扩大文件或工具权限。WorkBuddy 的文件输入为文本中的附件读取命令，不声称有原生文件上传接口。

From 0.4.5, voice runs on the phone without Local ASR or provider model discovery. Controls appear in an associated chat. General-file reads are agent tool actions and may require approval; file/tool permissions are not expanded. WorkBuddy receives a read-only attachment command in text, not a native file-upload operation.

OpenCode 参数为 provider/model、variant 和 prompt parts。ACP 修改模型后重新读取完整配置，再校验强度；权限和 mode 配置不会改变。当前模型不支持选项时，拒绝发送并提示刷新。本机 Qoder/TRAE 初始化均声明图片支持；实际 CLI 的会话目录读取被拒绝，模型切换仅通过协议 fixture 验证，未宣称真实账号推理验收。

OpenCode receives provider/model, variant and prompt parts. ACP refreshes the complete configuration after model changes, then validates thought level without changing permissions or mode. An unsupported dependent selection fails before prompting and requests a refresh. Installed Qoder/TRAE initialization declares image support, but native session listing is rejected; model switching has protocol-fixture coverage, not actual-account inference acceptance.

## 图片与文件 / Images and files

点击输入框右侧「＋」，选择照片、拍照或文件。附件卡片显示缩略图、名称、大小，发送前可移除；可只发附件，也可附文字。单文件最多 8 MiB，每条最多 6 个、合计 24 MiB，图片合计最多 8 MiB。相册/相机图片缩到最长边 2048px；后端校验 JPEG、PNG、GIF 及最多 2400 万像素，HEIC/WebP 不受支持时应转换为 JPEG/PNG。

Tap + to choose a photo, take a picture or select a file. Cards show previews, names and sizes, with removal before sending. Attachment-only messages are supported. Limits: 8 MiB per file, six files and 24 MiB per turn, with at most 8 MiB of images in total. Camera/gallery images are resized to 2048px. The server validates JPEG/PNG/GIF and a 24-megapixel ceiling; unsupported HEIC/WebP should be converted to JPEG/PNG.

上传使用现有 Local↔App 端到端加密 WS，在 64 KiB 分块内传输元数据和字节；Gateway 只转发密文。分块和元数据在 Local vault 中使用存储密钥加密，绑定来源设备、会话与授权项目；同一分块重试须逐字节一致。失败保留输入框草稿，可再次发送；已发送附件保存稳定 `agentdeck-file:` 引用，后续在同一会话或同项目、同 Agent 的后代续聊中可读取。别的配对设备不能通过上传附件接口读取该附件；原生 CLI 回显图片产生的媒体引用也保留原设备限制。

Metadata and bytes travel over the existing Local↔App end-to-end encrypted WS in 64 KiB chunks; Gateway relays ciphertext. Local encrypts chunks and metadata with its storage key, binding them to the originating device, chat and allowed project. Retrying a chunk requires identical bytes. Failures retain the composer draft. Sent attachments retain stable `agentdeck-file:` references and can be read in the same chat or its same-project/same-agent descendants. Other paired devices cannot fetch them through uploaded-attachment APIs. Native CLI image echoes retain the same originating-device restriction on their media references.

手机会话支持图片放大和文本文件预览（大文件预览前 128 KiB）；PDF 等二进制文件显示类型和大小，当前不提供其页面预览。

The phone offers image zoom and text previews (the first 128 KiB for large files). Binary formats such as PDF show file details; page rendering is not included yet.

图片使用 Codex 原生 image data URL、Claude/ACP base64 图片块或 OpenCode file part。普通文件不在后端创建明文工作副本，消息提供只读 `agentdeck input-file --data … --session … --upload …` 命令，从 0600 的 owner Unix socket 读取。`--format text` 默认读取前 64 KiB，可用 `--offset/--limit` 分页；`--format raw/base64` 用于二进制管道。公网 HTTP 不注册此读取端点，不支持任意路径、shell 或指定输出文件。

Images use native Codex image data URLs, Claude/ACP base64 image blocks or OpenCode file parts. General files do not create plaintext backend working copies. A message supplies an `agentdeck input-file --data … --session … --upload …` read-only command backed by the 0600 owner Unix socket. Text defaults to the first 64 KiB with byte offset/limit paging; raw/base64 support binary pipes. The public HTTP listener has no owner read endpoint; there is no arbitrary-path, shell or output-file operation.

手机草稿、图片缓存和录音在内存；系统文件/照片选择器可能生成暂存副本，读取后清理插件临时副本，不删除源文件。应用退出时内存草稿消失。模型输入进入原生 CLI 后，该 CLI 自身的日志/历史由其原有配置管理；若 agent 经正常工具权限将附件写进项目，项目副本也属于原有项目存储，不是 AgentDeck vault 的加密保护范围。

Phone drafts, image caches and recordings stay in memory. OS pickers may create temporary copies; plugin temporary copies are cleaned after reading without deleting originals. In-memory drafts are lost on app exit. Native CLI logging/history follows its own configuration. Files written into a project by approved agent tools belong to that project’s storage, outside AgentDeck vault encryption.

点击加号立即打开附件选择框并收起键盘，照片、拍照、文件在本地直接可选，没有加载请求。只有选中来源后才打开系统选择器。WorkBuddy 已知不支持图片，照片与拍照保持禁用，文件选择器中的图片也会提示更换普通文件。其他 Agent 的图片支持在点击发送后、创建续聊和上传前查询并校验，失败保留文字及附件草稿；普通文件选择不查询模型列表。

Plus immediately opens a local attachment sheet and dismisses the keyboard. Photo / Camera / File never wait for a network request; selecting a source opens the OS picker. WorkBuddy has no image input, so its photo/camera sources remain disabled and images chosen through Files are rejected. Other agents validate image support after Send, before conversation adoption or upload, retaining all drafts on failure. General file selection never queries a model list.

## 语音 / Voice

**0.4.41 起仅带语音版包含此功能；轻量版没有麦克风入口、语音模型、语音引擎或 RECORD_AUDIO 权限。** 可在设置 → 应用更新选择安装类型；同版本切换保留配对。 / **From 0.4.41 this section applies only to With voice. Lite excludes the microphone entry, speech models/engines and RECORD_AUDIO permission.** Choose the edition in App updates; switching at the same version retains pairing.

0.4.6 默认使用手机内置 Zipformer（int8 encoder/joiner + float32 decoder）流式识别，加 SenseVoiceSmall 整句校正。0.4.7 点击麦克风先切换为整块「按住说话」按钮，再长按大按钮录音，文字在原光标处持续更新；松开结束，向上滑动后松开取消。键盘图标切回文字，或点击识别文字直接编辑，草稿保留。取消恢复原草稿，迟到结果忽略；切后台或离开页面也取消。最多 60 秒，仅在长按时请求麦克风权限，预加载不打开麦克风。文字可编辑，识别不自动发送。

From 0.4.6, embedded Zipformer streaming (int8 encoder/joiner + float32 decoder) is followed by SenseVoiceSmall utterance correction. In 0.4.7, tap the microphone to reveal the full-width Hold to speak button, then hold that button for live text at the original cursor. Release to finish or slide upward before release to cancel. The keyboard icon or a tap on recognized text restores editing while preserving the draft. Cancellation restores the draft and ignores late results, including background/navigation cancellation. Recording is capped at sixty seconds. Only a hold requests microphone permission; preloading never opens it. Transcripts remain editable and are never automatically sent.

音频与识别结果只在手机内存中处理，不上传 Local、Gateway 或云语音服务，不保存录音工作文件；推理在独立 isolate，避免阻塞 UI。公开模型权重随 APK 提供并缓存到手机私有目录，SHA-256 在后台校验。模型首次加载需要几秒，App 启动时预加载；已打开的会话断网也能识别、编辑，发送仍需恢复加密连接。 / Audio and hypotheses remain in phone RAM without uploads or audio working files. Inference runs in a separate isolate. Public model weights ship with the APK, cache in app-private storage and have background SHA-256 verification. Initial loading takes seconds and starts when the app opens. An already-open chat can recognize/edit offline; sending still requires the encrypted connection.

模型二进制不入 Git；构建前运行 `python3 scripts/setup_client_voice.py` 获取固定版本并校验。约 299.7 MB 权重加上 native runtime 会增大安装包。来源和 Apache-2.0 说明见 [客户端模型许可](CLIENT_VOICE_LICENSE.md)。 / Model binaries are excluded from Git. Run the setup script before building to fetch the pinned assets. Roughly 299.7 MB of weights plus the native runtime increase the package size. See model/license provenance.

### 后端转写（旧客户端兼容） / Backend transcription for legacy clients

0.4.3/0.4.4 客户端及既有加密协议的 Local Whisper 路径继续兼容；0.4.6 的默认麦克风入口不调用该路径。 / Local Whisper and its encrypted protocol remain compatible with 0.4.3/0.4.4 clients. The default 0.4.6 microphone does not call that path.

安装一次本机离线服务（首次下载公开的 multilingual base 权重）：

Install the offline service once (the first setup downloads public multilingual base weights):

```sh
./scripts/setup_voice.sh
./artifacts/agentdeck serve \
  --voice-command-file "$HOME/.agentdeck-tools/voice/command.json" \
  --project-roots /your/projects \
  --public-url https://agent.example.com \
  --behind-proxy --listen 127.0.0.1:8443
```

将 voice-command-file 添加到原来的 serve 命令，其余 data、证书/代理、目录和网关配置保持原值。Relay 模式同样在 Local 配置，Gateway 不运行转写服务。配置文件须为 0600 JSON argv，绝对路径可执行程序，直接 exec，不经过 shell。服务读取 stdin 的单声道 16kHz PCM16 WAV，通过内存 BytesIO 送给 faster-whisper CPU int8，只输出 JSON 文字；不保存录音文件、不使用云 API/密钥。ASR 权重在本机缓存，下载后离线转写。每次转写限 55 秒，同一 Local 同时一条；请求与实时会话推送异步处理。

Add voice-command-file to your existing serve command, preserving data, certificates/proxy, roots and gateway configuration. Relay mode configures transcription on Local; Gateway does not transcribe. Configuration is a 0600 JSON argv with an absolute executable, executed directly without a shell. The service consumes mono 16kHz PCM16 WAV on stdin, sends in-memory BytesIO to faster-whisper CPU int8, and outputs JSON text. Audio files are not saved and cloud APIs/keys are unnecessary. Downloaded weights support offline operation. Each transcription has a 55-second timeout, with one active job per Local, asynchronous to live chat delivery.

录音也经配对加密通道分块上传，临时密文记录在转写结束或失败后删除。识别质量和耗时依赖口音、噪音及机器性能；中文结果转成简体，发送前可修改。

Audio uses paired encrypted chunk upload; temporary ciphertext records are deleted after transcription succeeds or fails. Accuracy and latency depend on speech, noise and workstation performance. Chinese output is converted to simplified Chinese and remains editable before sending.

## 协议来源 / Protocol sources

- [Codex app-server](https://developers.openai.com/codex/app-server)
- [Claude CLI](https://code.claude.com/docs/en/cli-reference), [model configuration](https://code.claude.com/docs/en/model-config)
- [OpenCode server](https://dev.opencode.ai/docs/server/), [ACP session configuration](https://agentclientprotocol.com/protocol/v1/session-config-options), [WorkBuddy API](https://open.workbuddy.cn/docs/openapi)
- [faster-whisper](https://github.com/SYSTRAN/faster-whisper)
- [record](https://pub.dev/packages/record), [file_picker](https://pub.dev/packages/file_picker), [image_picker](https://pub.dev/packages/image_picker)


## 实时语音 / Live voice

0.4.6 的默认路径：手机 PCM16 → 后台 isolate 的 Zipformer 流式识别 → 停顿/松开时在同一手机用 SenseVoiceSmall 重识别当前句子。语音在 RAM，取消或结束后释放，不走 HTTP/WSS、不存录音。配对和联网只用于会话同步/手动发送，识别可离线运行。长按时显示的文字是初稿，校正可能修改它；最终仍可编辑。校正按停顿分段，持续说话最长每 20 秒切段，总录音上限 60 秒。

The 0.4.6 default path is phone PCM16 → Zipformer streaming in a background isolate → SenseVoiceSmall correction of the current utterance at a pause/release. Audio stays in RAM and is discarded after finish/cancel, without HTTP/WSS or recording files. Pairing/network are needed for chat sync and explicit Send; recognition works offline. Live text is provisional and can change after correction; final text remains editable. Pauses segment correction, continuous speech segments after 20 seconds, and a hold is capped at 60 seconds.

量化 decoder 是 0.4.5 的严重错漏来源。0.4.6 保留 float32 decoder，encoder/joiner 仍用 int8。两模型使用 CPU 两线程，模型约 299.7 MB（未压缩）；均由构建脚本校验固定哈希。首次复制、校验及加载更久，App 启动时预加载。录音缓冲请求 3200 bytes（16 kHz 单声道约 100 ms），设备可能调整。

The quantized decoder caused severe recognition deletions in 0.4.5. Version 0.4.6 retains the float32 decoder with int8 encoder/joiner. Both models use two CPU threads. Uncompressed model assets total about 299.7 MB and are hash-verified by the build script. First copy/verification/loading takes longer; startup preloads them. Requested recording buffers are 3200 bytes, about 100 ms at 16 kHz mono; devices may adjust them.

准确率回归使用 6 句固定合成指令（142 个去标点、忽略大小写的中文字符/拉丁字母），覆盖普通中文和英文技术词混说。0.4.5 为 105 个编辑距离错误；只改 float32 decoder 为 29 个；加整句校正为 9 个。4 句普通中文的错误为 56 → 3 → 1。它是小样本回归，不能外推为用户真实口音、环境噪声或云语音的准确率。技术名词仍有误识别，不做未经证实的品牌名强制替换。静音、取消后重新录音和停顿分句另测。

The regression suite uses six fixed synthetic instructions: 142 Chinese characters/Latin letters after removing punctuation and case. Edit-distance errors were 105 in 0.4.5, 29 with only the float32 decoder, and 9 with utterance correction. For four general-Chinese sentences, errors were 56 → 3 → 1. This small regression sample does not establish accuracy for user accents/noise or parity with cloud speech. Technical names can still be wrong; no speculative brand-name replacement is applied. Silence, restart after cancel and paused utterances are checked separately.

可重复的 native 检查 / Reproducible native check (macOS synthetic voices, never opens microphone):

```sh
python3 scripts/setup_client_voice.py
python3 scripts/check_client_voice.py --dart /path/to/flutter/bin/dart
```

其他平台可提供非私人的单声道 16kHz WAV 与 `[{"id":"sample","path":"/absolute/audio.wav","text":"reference","kind":"public"}]` 给 `app/tool/check_client_voice.dart`。手机实测、安装包哈希和具体收尾时间记录在 [ACCEPTANCE.md](ACCEPTANCE.md)；0.4.5 的约 0.21 秒收尾只补上下文，不是 0.4.6 的整句校正时延。

On other platforms, supply non-private mono 16kHz WAV files and the above reference JSON to `app/tool/check_client_voice.dart`. Phone measurements, artifact hashes and release-tail timings are recorded in [ACCEPTANCE.md](ACCEPTANCE.md). The roughly 0.21-second tail in 0.4.5 only flushed decoder context and does not describe 0.4.6 correction latency.

旧 Local Whisper 上传/预览协议保留兼容，默认麦克风不使用它。 / Legacy Local Whisper upload/preview remains compatible; the default microphone does not use it.


0.4.6 GM1900 原生 release：上述 6 句实际运行同一手机 worker，错误仍为 9/142；预热后首段 430–759 ms，短句释放后的校正 233–369 ms。34.66 秒带停顿组合句为 11/142 错误，停顿处单块校正最多 428 ms。首次复制/校验/加载 12.75 秒。计时为非麦克风的合成 PCM 重放，从送入模型起，不包括长按触发、录音器启动/停止、用户语音或噪声。生产包约 274.7 MB；音频及测速界面不在生产包。

Native 0.4.6 release on GM1900 ran the same phone worker: 9/142 errors across the six sentences, 430–759 ms to first warmed partial text, and 233–369 ms correction after releasing short phrases. A 34.66-second combined sample with pauses had 11/142 errors and at most 428 ms compute on a pause-correction chunk. Initial copy/hash/load took 12.75 seconds. These are synthetic PCM replay measurements without a microphone, timed from model feeding and excluding hold detection/recorder startup-stop/user speech/noise. The production package is about 274.7 MB and contains neither fixture audio nor the measurement screen.


## 提问与原生选项 / Questions and native choices

Agent 提问显示为「需要你的回答」，与执行许可分开。完整展示题目、选项说明，按原生定义提供单选、多选和其他回答；没有默认替用户提交。多题须完成后一起提交。提交中禁用重复操作，失败保留当前选择。已在电脑或其他设备处理的提问会失效，后端会再次核对当前会话和项目，不能拿另一会话的提问 ID 代答。

Questions appear as “Your answer is needed”, separately from tool permissions. The app shows question text, option descriptions, native single/multiple selection and custom input where offered. No answer is auto-submitted. All questions are answered together; sending disables duplicate taps and errors preserve selections. The workstation rechecks the original session, project and pending request before replying.

| Agent / 入口 | 答案回传 / Native response |
| --- | --- |
| Codex 后端管理的 app-server 会话 / Local-managed app-server | `item/tool/requestUserInput`，按问题 ID 回传 `answers: string[]` / Question IDs and answer arrays |
| Codex Desktop 原会话 / Original Desktop chat | 从 owner 的实时 `requests` 读取，重新核对后通过 `thread-follower-submit-user-input` 回传，保持原会话；不启动替代 CLI / Read the owner's requests, revalidate, reply through the native follower interface; no replacement CLI |
| Codex Desktop 异步选择题 / Desktop asynchronous choices | 从原生 `agentMessage.questions` 读取题目和选项；回答通过原 owner 的普通用户输入发送，执行中 steer、结束后同 ID 续聊。保持原生 questionItemId，不获取写锁、不启动 CLI。 / Read native message questions; send ordinary human replies through the original owner, steering an active turn or continuing the same ID after completion. Preserve native question IDs without another writer. |
| Claude Code CLI / Desktop | `AskUserQuestion`，支持 `multiSelect`，在 SDK 边界按原生 string answer map 回传 / Preserves multi-select through the SDK string answer map |
| OpenCode | `question.asked/replied/rejected`、`GET /question`、`POST /question/:id/reply`；按题目原顺序传二维答案数组 / Original-order answer arrays and pending recovery on reconnect |
| Qoder / TRAE ACP | 完整展示原生 `request_permission.options`，只回传用户明确选择的原生 `optionId`，不伪造 Agent 未提供的提问接口 / Exact native permission options and explicit option IDs; no invented question endpoint |

异步选择题支持原生选项及其他回答，多个题目一次提交；在电脑回答后，手机卡片同步收起。回答接收未确认时只核对原生记录，不自动重复发送；已确认的相同回答可安全重复提交请求而不再发送给 Agent。跳过异步回答仅在 App 收起卡片，不向 Agent 发送“同意”、不停止任务。问题和回答仍加密存储。当前 0.4.45 App 已有这些卡片的渲染能力，更新 S-LOCAL 即可，无需重新安装 APK。

Async choices support native options and custom text, with grouped submission and desktop-to-phone resolution. An unknown acknowledgement is confirmed against native records without resending; confirmed identical retries do not send again. Skipping only dismisses the App card and neither approves a tool nor stops execution. Storage remains encrypted. The existing 0.4.45 App already renders these cards; upgrade S-LOCAL without reinstalling the APK.

工作站仍通过现有加密通道传输提问和答案，网关不解析明文。本次 Claude 只做协议测试，不启用或操作 Desktop 调试入口。较新的 Codex 原生 Option Picker / 动态工具和 MCP 任意表单不等同于 `requestUserInput`；当前 Desktop follower 接口未提供这些类型的通用回答入口，不宣称支持。WorkBuddy 未提供的交互也不伪装成可回答。

Questions and answers use the existing encrypted transport; the gateway does not read plaintext. Claude validation for this change uses protocol fixtures, without enabling its Desktop debugger. Newer Codex Option Picker/dynamic tools and arbitrary MCP forms are distinct protocols; the current Desktop follower API has no generic reply route for them, so they are not claimed as supported. WorkBuddy interactions without an exposed API are not presented as answerable.


## 执行过程按需读取 / Execution details on demand

0.4.46 App 与更新后的 S-LOCAL 配合使用：打开会话、历史翻页和实时推送只传思考/工具/文件变更的短摘要。展开「执行过程」只列摘要；点击具体一项才拉取该记录的最新全文，加载中转圈，失败可重试。已展开项继续同步更新；收起后释放详情，后续更新不会在后台重新下载全文。消息、图片、提问和执行许可维持原交互。

With App 0.4.46 and the updated S-LOCAL, initial history, paging and live pushes carry short execution summaries. Expanding the group lists summaries; opening a specific item fetches its latest body with progress/retry. Open items stay current; closing releases the body and stops further detail fetches. Messages, media, questions and permissions retain their normal behavior.

提问卡按原生来源位置展示，不再因为后端补发而堆到最后；没有独立文字消息的提问也保留前序位置。展示顺序与持久化游标分开，保留完整历史和分页边界。旧缓存的已处理异步卡片在读取时补齐位置。

Question cards follow their native source position, including questions without a separate text message and older resolved cached cards. Presentation order is independent of durable paging coordinates, preserving history boundaries.

协议兼容：新 App 对 watch/history/history_after/history_at 请求声明 `compact: true`；执行摘要带 `detail: true`，具体项经现有加密通道调用 `item_detail(session,item,kind)`。仅允许思考、工具调用/输出及文件变更，不开放任意 RPC 或文件读取。旧 App 不声明 compact，继续收到原格式；旧 Local 不支持 compact 时，新 App 仍兼容全文，但需要升级 Local 才有按需加载的流量收益。

New clients opt into compact history/live snapshots and retrieve individual execution details over the existing encrypted channel. The endpoint accepts only supported execution record kinds and preserves exact session/item identity. Older clients retain the full protocol; older Locals remain usable but require an upgrade for deferred-transfer savings.
