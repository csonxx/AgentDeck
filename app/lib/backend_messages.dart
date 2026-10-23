// App-owned backend messages. Keep wire values and conversation bodies unchanged.
const backendUiMessages = <String, List<String>>{
  "工作站直连已关闭": [
    "Direct connections are disabled on the workstation.",
    "工作站直連已關閉",
    "Прямые соединения отключены на рабочей станции.",
  ],
  "直连协商未完成，继续使用网关中继": [
    "Direct negotiation did not complete; using gateway relay.",
    "直連協商未完成，繼續使用閘道中繼",
    "Прямое соединение не установлено; используется ретрансляция.",
  ],
  "直连协商忙，继续使用网关中继": [
    "Direct negotiation is busy; using gateway relay.",
    "直連協商忙，繼續使用閘道中繼",
    "Установка прямого соединения занята; используется ретрансляция.",
  ],
  "直连尚未就绪，继续使用网关中继": [
    "Direct connection is not ready; using gateway relay.",
    "直連尚未就緒，繼續使用閘道中繼",
    "Прямое соединение не готово; используется ретрансляция.",
  ],
  "暂时无法读取 Codex 重置卡，请检查电脑登录和 CLI 版本": [
    "Could not read Codex reset cards. Check desktop login and CLI version.",
    "暫時無法讀取 Codex 重置卡，請檢查電腦登入和 CLI 版本",
    "Не удалось прочитать карты Codex. Проверьте вход и версию CLI.",
  ],
  "Codex 未返回可核对的账号，请更新电脑 CLI": [
    "Codex returned no verifiable account. Update the desktop CLI.",
    "Codex 未傳回可核對的帳號，請更新電腦 CLI",
    "Codex не вернул проверяемый аккаунт. Обновите CLI.",
  ],
  "无效的重置卡请求": [
    "Invalid reset card request.",
    "無效的重置卡請求",
    "Недопустимый запрос карты сброса.",
  ],
  "电脑 Codex 账号已变化，请刷新后重新确认": [
    "The desktop Codex account changed. Refresh and confirm again.",
    "電腦 Codex 帳號已變更，請重新整理後再次確認",
    "Аккаунт Codex изменился. Обновите данные и подтвердите снова.",
  ],
  "当前没有可用重置卡": [
    "No reset cards are available.",
    "目前沒有可用重置卡",
    "Нет доступных карт сброса.",
  ],
  "当前没有可重置的核心额度，五小时或每周额度需剩余不超过 10%": [
    "A five-hour or weekly core limit must have 10% or less remaining to reset.",
    "目前沒有可重置的核心額度，五小時或每週額度需剩餘不超過 10%",
    "Для сброса должно остаться не более 10% пятичасового или недельного основного лимита.",
  ],
  "这张重置卡已不可用，请刷新": [
    "This reset card is unavailable. Refresh to check.",
    "這張重置卡已無法使用，請重新整理",
    "Эта карта недоступна. Обновите данные.",
  ],
  "上次重置结果尚未确认，请先核对原请求": [
    "The previous reset is unconfirmed. Check the original request first.",
    "上次重置結果尚未確認，請先核對原請求",
    "Прошлый сброс не подтверждён. Сначала проверьте исходный запрос.",
  ],
  "重置确认保存失败，尚未使用重置卡": [
    "Could not save confirmation. No reset card has been used.",
    "重置確認儲存失敗，尚未使用重置卡",
    "Не удалось сохранить подтверждение. Карта не использована.",
  ],
  "请先明确确认使用一张重置卡": [
    "Explicitly confirm using one reset card first.",
    "請先明確確認使用一張重置卡",
    "Сначала подтвердите использование одной карты.",
  ],
  "重置确认不属于当前手机或已失效": [
    "This confirmation belongs to another phone or is no longer valid.",
    "重置確認不屬於目前手機或已失效",
    "Подтверждение относится к другому телефону или недействительно.",
  ],
  "请使用原重置请求核对结果，勿新建请求": [
    "Use the original reset request to check the result. Do not create a new request.",
    "請使用原重置請求核對結果，勿新增請求",
    "Проверьте результат через исходный запрос. Не создавайте новый.",
  ],
  "确认已过期，请重新查看额度后确认": [
    "Confirmation expired. Check usage and confirm again.",
    "確認已過期，請重新查看額度後確認",
    "Подтверждение истекло. Проверьте лимиты и подтвердите снова.",
  ],
  "电脑 Codex 账号已变化，未使用重置卡": [
    "The desktop Codex account changed. No reset card was used.",
    "電腦 Codex 帳號已變更，未使用重置卡",
    "Аккаунт Codex изменился. Карта не использована.",
  ],
  "重置回执保存失败，未提交": [
    "Could not save the reset receipt. Nothing was submitted.",
    "重置回執儲存失敗，未提交",
    "Не удалось сохранить запись сброса. Запрос не отправлен.",
  ],
  "历史游标无效": [
    "Invalid history cursor.",
    "歷史游標無效",
    "Недопустимый курсор истории.",
  ],
  "无法核对 Codex 历史账号": [
    "Could not verify the Codex history account.",
    "無法核對 Codex 歷史帳號",
    "Не удалось проверить аккаунт истории Codex.",
  ],
  "Codex 历史账号暂不可用": [
    "The Codex history account is unavailable.",
    "Codex 歷史帳號暫時無法使用",
    "Аккаунт истории Codex недоступен.",
  ],
  "重置卡历史暂不可用，请检查电脑登录或稍后重试": [
    "Reset card history is unavailable. Check desktop login or try later.",
    "重置卡歷史暫時無法使用，請檢查電腦登入或稍後重試",
    "История карт недоступна. Проверьте вход на компьютере или повторите позже.",
  ],
  "重置卡历史格式暂不支持": [
    "This reset card history format is not supported yet.",
    "重置卡歷史格式暫不支援",
    "Этот формат истории карт пока не поддерживается.",
  ],
  "重置卡历史游标无效": [
    "Invalid reset card history cursor.",
    "重置卡歷史游標無效",
    "Недопустимый курсор истории карт.",
  ],
  "Codex 重置卡请求未授权": [
    "The Codex reset card request is not authorized.",
    "Codex 重置卡請求未授權",
    "Запрос карты Codex не авторизован.",
  ],

  "工作站正在更新，请稍后重试": [
    "The workstation is updating. Please try again shortly.",
    "工作站正在更新，請稍後重試",
    "Рабочая станция обновляется. Повторите попытку немного позже.",
  ],
  "提问回答格式无效": [
    "Invalid question answer format.",
    "提問回答格式無效",
    "Неверный формат ответа.",
  ],
  "提问已过期": ["This question has expired.", "提問已過期", "Срок ответа истёк."],
  "请回答每个问题，并核对单选或多选": [
    "Answer every question and check single or multiple selection.",
    "請回答每個問題，並核對單選或多選",
    "Ответьте на все вопросы и проверьте выбор вариантов.",
  ],
  "回答中有重复选项": [
    "The answer contains duplicate choices.",
    "回答中有重複選項",
    "Ответ содержит повторяющиеся варианты.",
  ],
  "请选择提问提供的选项": [
    "Choose one of the offered options.",
    "請選擇提問提供的選項",
    "Выберите из предложенных вариантов.",
  ],
  "回答包含不属于此提问的问题": [
    "The answer includes an unrelated question.",
    "回答包含不屬於此提問的問題",
    "Ответ относится к другому вопросу.",
  ],
  "提问题目标识重复": [
    "The agent returned duplicate question IDs.",
    "提問題目識別碼重複",
    "Агент вернул повторяющиеся ID вопросов.",
  ],
  "提问已过期或不属于当前会话": [
    "The question expired or belongs to another chat.",
    "提問已過期或不屬於目前會話",
    "Вопрос истёк или относится к другому чату.",
  ],
  "提问或审批已过期": [
    "The question or approval has expired.",
    "提問或審批已過期",
    "Вопрос или запрос разрешения истёк.",
  ],
  "电脑 Codex 提问状态读取失败，请刷新": [
    "Could not read the desktop Codex question. Refresh to retry.",
    "電腦 Codex 提問狀態讀取失敗，請重新整理",
    "Не удалось прочитать вопрос Codex. Обновите чат.",
  ],
  "电脑 Codex 提问会话或项目不匹配": [
    "The desktop Codex question does not match this chat or project.",
    "電腦 Codex 提問會話或專案不符",
    "Вопрос Codex относится к другому чату или проекту.",
  ],
  "提问已过期或已在其他设备处理，请刷新": [
    "This question expired or was handled on another device. Refresh the chat.",
    "提問已過期或已在其他裝置處理，請重新整理",
    "Вопрос истёк или обработан на другом устройстве. Обновите чат.",
  ],
  "电脑 Codex 尚未确认回答，请刷新核对，勿重复提交": [
    "Desktop Codex has not confirmed the answer. Refresh before submitting again.",
    "電腦 Codex 尚未確認回答，請重新整理核對，勿重複提交",
    "Codex ещё не подтвердил ответ. Обновите чат, прежде чем отправлять повторно.",
  ],
  "当前电脑 Codex 接口不支持提问回答": [
    "This desktop Codex interface cannot accept question answers.",
    "目前電腦 Codex 介面不支援提問回答",
    "Этот интерфейс Codex не поддерживает ответы на вопросы.",
  ],
  "审批选项格式无效": [
    "Invalid permission choice format.",
    "審批選項格式無效",
    "Неверный формат разрешения.",
  ],
  "审批决定与选项不一致": [
    "The permission decision does not match the selected option.",
    "審批決定與選項不一致",
    "Решение не соответствует выбранному разрешению.",
  ],
  "审批选项已过期": [
    "This permission option has expired.",
    "審批選項已過期",
    "Этот вариант разрешения истёк.",
  ],
  "Claude 不支持此图片格式，消息未发送": [
    "Claude does not support this image format. Message not sent.",
    "Claude 不支援此圖片格式，訊息未傳送",
    "Claude не поддерживает этот формат изображения. Сообщение не отправлено.",
  ],
  "Claude 原会话操作未确认，请刷新核对；审批可能已过期": [
    "The operation in the original Claude chat is unconfirmed. Refresh to check; the approval may have expired.",
    "Claude 原會話操作未確認，請重新整理核對；審批可能已過期",
    "Операция в исходном чате Claude не подтверждена. Обновите чат; запрос разрешения мог истечь.",
  ],
  "Claude 审批结果尚未确认，请刷新核对": [
    "The Claude approval result is unconfirmed. Refresh to check.",
    "Claude 審批結果尚未確認，請重新整理核對",
    "Результат разрешения Claude не подтверждён. Обновите чат для проверки.",
  ],
  "审批回答格式无效": [
    "Invalid approval answer format.",
    "審批回答格式無效",
    "Недопустимый формат ответа на запрос разрешения.",
  ],
  "当前 Claude Desktop 不支持图片直发，消息未发送": [
    "This Claude Desktop version does not support direct image input. Message not sent.",
    "目前 Claude Desktop 不支援圖片直傳，訊息未傳送",
    "Эта версия Claude Desktop не поддерживает прямую отправку изображений. Сообщение не отправлено.",
  ],
  "当前 Claude Desktop 不支持此会话操作": [
    "This Claude Desktop version does not support this chat operation.",
    "目前 Claude Desktop 不支援此會話操作",
    "Эта версия Claude Desktop не поддерживает данную операцию с чатом.",
  ],

  "Claude 本机直发连接未开启；请在电脑 Claude 的 Developer 菜单开启 Enable Main Process Debugger，消息未发送": [
    "Claude local direct input is unavailable. Enable Main Process Debugger in Desktop's Developer menu. Message not sent.",
    "Claude 本機直傳連線未開啟；請在電腦 Claude 的 Developer 選單開啟 Enable Main Process Debugger，訊息未傳送",
    "Локальная отправка в Claude недоступна. Включите Enable Main Process Debugger в меню Developer на компьютере. Сообщение не отправлено.",
  ],
  "Claude 原会话尚未确认收到这条消息；不会重复发送，请稍后刷新核对": [
    "Reception in the original Claude chat is unconfirmed. This message will not be resent; refresh the chat to check.",
    "Claude 原會話尚未確認收到這則訊息；不會重複傳送，請稍後重新整理核對",
    "Получение в исходном чате Claude не подтверждено. Повторной отправки не будет; обновите чат для проверки.",
  ],
  "Claude Desktop 会话目录无效或权限不安全": [
    "The Claude Desktop session directory is invalid or has unsafe permissions.",
    "Claude Desktop 會話目錄無效或權限不安全",
    "Каталог сеансов Claude Desktop неверен или имеет небезопасные права.",
  ],
  "无法唯一确认 Claude Desktop 原会话和项目，消息未发送": [
    "Cannot uniquely verify the original Claude Desktop chat and project. Message not sent.",
    "無法唯一確認 Claude Desktop 原會話和專案，訊息未傳送",
    "Не удалось однозначно проверить исходный чат и проект Claude Desktop. Сообщение не отправлено.",
  ],
  "Claude Desktop 原会话记录不存在，消息未发送": [
    "The original Claude Desktop chat record is missing. Message not sent.",
    "Claude Desktop 原會話記錄不存在，訊息未傳送",
    "Запись исходного чата Claude Desktop отсутствует. Сообщение не отправлено.",
  ],
  "Claude Desktop 本机直发图片输入尚未验收，消息未发送": [
    "Image input through Claude Desktop local direct input has not passed acceptance. Message not sent.",
    "Claude Desktop 本機直傳圖片輸入尚未驗收，訊息未傳送",
    "Ввод изображений через локальную отправку Claude Desktop ещё не проверен. Сообщение не отправлено.",
  ],
  "无法确认 Claude Desktop 原会话，消息未发送": [
    "Cannot verify the original Claude Desktop chat. Message not sent.",
    "無法確認 Claude Desktop 原會話，訊息未傳送",
    "Не удалось проверить исходный чат Claude Desktop. Сообщение не отправлено.",
  ],
  "此原会话由另一执行端持有，消息未发送；不会新建并行执行端": [
    "Another executor owns this chat. Message not sent; no parallel executor will be created.",
    "此原會話由另一執行端持有，訊息未傳送；不會新建平行執行端",
    "Этот чат занят другим исполнителем. Сообщение не отправлено; параллельный исполнитель не будет создан.",
  ],
  "请在电脑 Claude 原会话中修改模型参数后再发送": [
    "Change model settings in the original desktop Claude chat before sending.",
    "請在電腦 Claude 原會話中修改模型參數後再傳送",
    "Измените параметры модели в исходном чате Claude на компьютере перед отправкой.",
  ],
  "Claude Desktop 原会话身份已变化，消息未发送": [
    "The original Claude Desktop chat identity changed. Message not sent.",
    "Claude Desktop 原會話身分已變更，訊息未傳送",
    "Идентичность исходного чата Claude Desktop изменилась. Сообщение не отправлено.",
  ],
  "Claude 原会话执行端已变化，消息未发送": [
    "The executor of the original Claude chat changed. Message not sent.",
    "Claude 原會話執行端已變更，訊息未傳送",
    "Исполнитель исходного чата Claude изменился. Сообщение не отправлено.",
  ],
  "Claude 官方 CLI 远程接口会将消息标记为其他会话请求，尚不能用于手机作为用户直发 Desktop 原会话；消息未发送": [
    "Claude's remote CLI treats input as a peer request and cannot deliver phone user input to the original Desktop chat. Message not sent.",
    "Claude 官方 CLI 遠端介面會將訊息標記為其他會話請求，尚不能用於手機作為使用者直接傳送至 Desktop 原會話；訊息未傳送",
    "Удалённый CLI Claude считает ввод запросом другого агента и пока не подходит для ввода пользователя с телефона в исходный чат Desktop. Сообщение не отправлено.",
  ],
  "电脑 Claude 原会话尚未开启 Remote Control；请在该会话启用远程连接后发送": [
    "Enable Remote Control in the original desktop Claude chat before sending.",
    "電腦 Claude 原會話尚未開啟 Remote Control；請在該會話啟用遠端連線後傳送",
    "Включите Remote Control в исходном чате Claude на компьютере перед отправкой.",
  ],
  "Claude 原会话投递结果尚未确认，请核对原会话；不会自动重复发送": [
    "Claude delivery is unconfirmed. Check the original chat; this message will not be resent automatically.",
    "Claude 原會話投遞結果尚未確認，請核對原會話；不會自動重複傳送",
    "Доставка в Claude не подтверждена. Проверьте исходный чат; автоматической повторной отправки не будет.",
  ],
  "消息编号已用于不同内容": [
    "This message ID was already used for different content.",
    "訊息編號已用於不同內容",
    "Этот идентификатор сообщения уже использован для другого содержимого.",
  ],
  "消息为空、编号无效或超过 64 KiB": [
    "The message is empty, its ID is invalid, or it exceeds 64 KiB.",
    "訊息為空、編號無效或超過 64 KiB",
    "Сообщение пустое, идентификатор неверен или размер превышает 64 КиБ.",
  ],
  "Claude 原会话的远程连接标识或目录权限无效": [
    "The Claude remote connection ID or registry directory permissions are invalid.",
    "Claude 原會話的遠端連線識別碼或目錄權限無效",
    "Неверный идентификатор удалённого подключения Claude или права каталога реестра.",
  ],
  "Claude 原会话与授权项目不匹配": [
    "The original Claude chat does not match the authorized project.",
    "Claude 原會話與授權專案不符",
    "Исходный чат Claude не соответствует разрешённому проекту.",
  ],
  "此 Claude 远程接口使用原会话的模型参数；请在电脑原会话修改": [
    "This Claude remote interface uses the original chat model settings. Change them in the desktop chat.",
    "此 Claude 遠端介面使用原會話的模型參數；請在電腦原會話修改",
    "Этот удалённый интерфейс Claude использует настройки модели исходного чата. Измените их на компьютере.",
  ],
  "当前 Claude 远程发送接口尚不支持图片输入": [
    "This Claude remote sender does not yet support image input.",
    "目前 Claude 遠端傳送介面尚不支援圖片輸入",
    "Этот интерфейс удалённой отправки Claude пока не поддерживает изображения.",
  ],
  "Claude 原会话连接已变化，消息未发送": [
    "The original Claude connection changed; message not sent.",
    "Claude 原會話連線已變更，訊息未傳送",
    "Подключение исходного чата Claude изменилось; сообщение не отправлено.",
  ],
  "无法启动 Claude 远程发送客户端，消息未发送": [
    "Unable to start the Claude remote sender; message not sent.",
    "無法啟動 Claude 遠端傳送客戶端，訊息未傳送",
    "Не удалось запустить удалённый клиент Claude; сообщение не отправлено.",
  ],
  "Claude 进程记录权限不安全": [
    "The Claude process registry file permissions are unsafe.",
    "Claude 程序記錄權限不安全",
    "Небезопасные права файла реестра процессов Claude.",
  ],
  "Claude 桌面原进程不能交接；请使用原会话的 Remote Control 连接": [
    "The original Claude Desktop process cannot be handed off. Use its Remote Control connection.",
    "Claude 桌面原程序不能交接；請使用原會話的 Remote Control 連線",
    "Исходный процесс Claude Desktop нельзя передать. Используйте его подключение Remote Control.",
  ],
  "当前任务正在执行，完成后可修改模型参数": [
    "The task is running. Change model parameters after it finishes.",
    "目前任務正在執行，完成後可修改模型參數",
    "Задача выполняется. Измените параметры модели после завершения.",
  ],
  "此电脑 Claude 接口未提供模型参数修改，请在电脑会话中修改": [
    "This desktop Claude interface cannot change model parameters. Change them in the desktop chat.",
    "此電腦 Claude 介面未提供模型參數修改，請在電腦會話中修改",
    "Интерфейс Claude не поддерживает изменение параметров модели. Измените их на компьютере.",
  ],

  "Claude 原会话仍在执行，尚未交接": [
    "The original Claude executor is still running and has not handed off.",
    "Claude 原會話仍在執行，尚未交接",
    "Исходный исполнитель Claude ещё работает; передача не завершена.",
  ],
  "Claude 原会话执行交接失败，消息未发送": [
    "Failed to hand off the original Claude executor; message not sent.",
    "Claude 原會話執行交接失敗，訊息未傳送",
    "Не удалось передать исходный чат Claude; сообщение не отправлено.",
  ],
  "Claude 原会话执行端发生变化，消息未发送": [
    "The original Claude executor changed; message not sent.",
    "Claude 原會話執行端發生變化，訊息未傳送",
    "Исполнитель исходного чата Claude изменился; сообщение не отправлено.",
  ],
  "Claude 原会话尚未完成执行交接，消息未发送": [
    "The original Claude executor has not finished handing off; message not sent.",
    "Claude 原會話尚未完成執行交接，訊息未傳送",
    "Передача исходного чата Claude ещё не завершена; сообщение не отправлено.",
  ],

  "请选择可用的 Claude 会话": [
    "Choose an available Claude chat.",
    "請選擇可用的 Claude 會話",
    "Выберите доступный чат Claude.",
  ],
  "无法确认 Claude 会话占用状态，请稍后重试": [
    "Unable to verify the Claude chat owner. Try again shortly.",
    "無法確認 Claude 會話佔用狀態，請稍後重試",
    "Не удалось проверить владельца чата Claude. Повторите позже.",
  ],
  "操作回执保存失败，请刷新核对，勿重复发送": [
    "Could not save the receipt. Refresh to verify; do not resend.",
    "操作回執儲存失敗，請重新整理核對，勿重複傳送",
    "Не удалось сохранить подтверждение. Обновите чат для проверки; не отправляйте повторно.",
  ],
  "电脑 Claude 原生输入桥接与会话不匹配": [
    "The native Claude input bridge does not match this chat.",
    "電腦 Claude 原生輸入橋接與會話不符",
    "Мост ввода Claude не соответствует этому чату.",
  ],
  "电脑 Claude 原生输入桥接状态无效，请在电脑检查": [
    "The native Claude input bridge state is invalid. Check the workstation.",
    "電腦 Claude 原生輸入橋接狀態無效，請在電腦檢查",
    "Некорректное состояние моста Claude. Проверьте компьютер.",
  ],
  "Claude 原生输入桥接已安装，请在当前任务结束后重新启动电脑 Claude 并打开原会话": [
    "The native Claude input bridge is installed. After the current task finishes, restart Claude Desktop and open the original chat.",
    "Claude 原生輸入橋接已安裝，請在目前任務結束後重新啟動電腦 Claude 並開啟原會話",
    "Мост ввода Claude установлен. После завершения задачи перезапустите Claude Desktop и откройте исходный чат.",
  ],
  "电脑 Claude 原会话需要原生输入桥接，请在本机安装后重新打开原会话": [
    "This desktop Claude chat needs the native input bridge. Install it on the workstation, then reopen the original chat.",
    "電腦 Claude 原會話需要原生輸入橋接，請在本機安裝後重新開啟原會話",
    "Для этого чата нужен мост ввода Claude. Установите его на компьютере и снова откройте исходный чат.",
  ],
  "电脑 Claude 原生输入未确认，请刷新核对，勿重复发送": [
    "Native Claude input is unconfirmed. Refresh to check; do not resend.",
    "電腦 Claude 原生輸入未確認，請重新整理核對，勿重複傳送",
    "Ввод Claude не подтверждён. Обновите чат для проверки; не отправляйте повторно.",
  ],
  "电脑 Claude 尚未确认接收；当前会话空闲，请检查 crossSessionInbound 收信设置。消息可能被暂存，勿重复发送": [
    "Claude has not confirmed receipt. The chat is idle; check crossSessionInbound. The message may be held. Do not resend.",
    "電腦 Claude 尚未確認接收；目前會話空閒，請檢查 crossSessionInbound 收信設定。訊息可能被暫存，勿重複傳送",
    "Claude не подтвердил получение. Чат не занят; проверьте crossSessionInbound. Сообщение могло быть отложено. Не отправляйте повторно.",
  ],

  "电脑 Codex 尚未加载原会话": [
    "Codex has not loaded the original chat yet.",
    "電腦 Codex 尚未載入原會話",
    "Codex ещё не загрузил исходный чат.",
  ],
  "Codex 原会话 ID 无效，无法自动唤起": [
    "The original Codex chat ID is invalid.",
    "Codex 原會話 ID 無效，無法自動喚起",
    "Недопустимый ID исходного чата Codex.",
  ],
  "无法自动唤起电脑 Codex 原会话，请检查桌面端是否正常运行": [
    "Could not open the original Codex chat. Check the desktop app.",
    "無法自動喚起電腦 Codex 原會話，請檢查桌面端是否正常執行",
    "Не удалось открыть исходный чат Codex. Проверьте приложение на компьютере.",
  ],
  "电脑 Codex 已尝试加载原会话，但执行端尚未就绪；消息未发送，请稍后重试": [
    "Codex tried to load the original chat but is not ready. The message was not sent; try again shortly.",
    "電腦 Codex 已嘗試載入原會話，但執行端尚未就緒；訊息未傳送，請稍後重試",
    "Codex попытался загрузить исходный чат, но пока не готов. Сообщение не отправлено; повторите позже.",
  ],

  "已向电脑 Claude 原会话投递，但尚未确认接收；请检查电脑的消息接收设置或等待当前工具结束，勿重复发送": [
    "Submitted to the original Claude chat, but receipt is unconfirmed. Check inbound settings or wait for the current tool. Do not resend.",
    "已向電腦 Claude 原會話投遞，但尚未確認接收；請檢查電腦的訊息接收設定或等待目前工具結束，勿重複傳送",
    "Отправлено в исходный чат Claude, получение не подтверждено. Проверьте приём сообщений или дождитесь инструмента. Не отправляйте повторно.",
  ],
  "电脑 Claude 原会话的模型和 effort 请在电脑设置；手机请选择「沿用当前会话设置」": [
    "Set the original Claude chat model and effort on the computer; select Inherit current chat settings on mobile.",
    "電腦 Claude 原會話的模型和 effort 請在電腦設定；手機請選擇「沿用目前會話設定」",
    "Модель и effort исходного чата Claude задайте на компьютере; на телефоне выберите настройки агента по умолчанию.",
  ],
  "Claude 进程目录权限不安全，无法连接原会话": [
    "Claude process registry permissions are unsafe. Cannot connect to the original chat.",
    "Claude 進程目錄權限不安全，無法連接原會話",
    "Небезопасные права каталога процессов Claude. Подключение невозможно.",
  ],
  "多个 Claude 进程持有同一会话，请先在电脑确认": [
    "Multiple Claude processes own this chat. Check the computer first.",
    "多個 Claude 進程持有同一會話，請先在電腦確認",
    "Этот чат открыт несколькими процессами Claude. Проверьте компьютер.",
  ],
  "电脑 Claude 未提供兼容的原会话消息接口，请更新并重新打开该会话": [
    "Claude has no compatible original-chat inbox. Update Claude and reopen the chat.",
    "電腦 Claude 未提供相容的原會話訊息介面，請更新並重新開啟該會話",
    "Нет совместимого интерфейса чата Claude. Обновите Claude и откройте чат заново.",
  ],
  "电脑 Claude 原会话消息接口权限不安全或已断开": [
    "The original Claude inbox is disconnected or has unsafe permissions.",
    "電腦 Claude 原會話訊息介面權限不安全或已斷開",
    "Интерфейс исходного чата Claude отключён или имеет небезопасные права.",
  ],
  "电脑 Claude 原会话消息接口暂不支持图片输入；请在电脑发送图片": [
    "The original Claude inbox does not support image input yet. Send images on the computer.",
    "電腦 Claude 原會話訊息介面暫不支援圖片輸入；請在電腦傳送圖片",
    "Интерфейс исходного чата Claude пока не поддерживает изображения. Отправьте их с компьютера.",
  ],
  "电脑 Claude 原会话消息接口已断开，请重试": [
    "The original Claude inbox disconnected. Please retry.",
    "電腦 Claude 原會話訊息介面已斷開，請重試",
    "Интерфейс исходного чата Claude отключился. Повторите попытку.",
  ],
  "电脑 Claude 已接收，历史同步暂未完成，请刷新会话，勿重复发送": [
    "Claude received the message. History sync is incomplete; refresh the chat without resending.",
    "電腦 Claude 已接收，歷史同步暫未完成，請重新整理會話，勿重複傳送",
    "Claude получил сообщение. История ещё синхронизируется; обновите чат без повторной отправки.",
  ],
  "请在电脑 Claude 原会话中处理停止或工具审批": [
    "Handle stopping and tool approval in the original Claude chat on the computer.",
    "請在電腦 Claude 原會話中處理停止或工具審批",
    "Остановку и разрешения инструментов обрабатывайте в исходном чате Claude на компьютере.",
  ],
  "此 Claude 会话已从桌面目录移除或归档": [
    "This Claude chat was removed from the desktop catalog or archived.",
    "此 Claude 會話已從桌面目錄移除或封存",
    "Этот чат Claude удалён из каталога или архивирован.",
  ],
  "此会话保留在 Claude 桌面目录中，但本机没有对应的日志，暂时无法读取历史或从手机续聊": [
    "This chat is listed on Claude Desktop, but its local log is missing. History and mobile continuation are unavailable.",
    "此會話保留在 Claude 桌面目錄中，但本機沒有對應的日誌，暫時無法讀取歷史或從手機續聊",
    "Чат есть в Claude Desktop, но локальный журнал отсутствует. История и продолжение на телефоне недоступны.",
  ],
  "Claude 本机会话日志暂时无法读取": [
    "The local Claude chat log is temporarily unreadable.",
    "Claude 本機會話日誌暫時無法讀取",
    "Локальный журнал чата Claude временно недоступен.",
  ],
  "此队列消息包含特殊上下文，请在电脑操作": [
    "This queued message has special context. Manage it on the computer.",
    "此佇列訊息包含特殊上下文，請在電腦操作",
    "Это сообщение содержит особый контекст. Управляйте им на компьютере.",
  ],
  "电脑队列读取失败，请更新或重新连接 Codex": [
    "Could not read the desktop queue. Update or reconnect Codex.",
    "電腦佇列讀取失敗，請更新或重新連線 Codex",
    "Не удалось прочитать очередь. Обновите Codex или подключитесь заново.",
  ],
  "电脑队列已变化，请刷新后重试": [
    "The desktop queue changed. Refresh and try again.",
    "電腦佇列已變更，請重新整理後再試",
    "Очередь изменилась. Обновите её и повторите попытку.",
  ],
  "电脑队列不支持此附件类型": [
    "The desktop queue does not support this attachment type.",
    "電腦佇列不支援此附件類型",
    "Очередь не поддерживает этот тип вложения.",
  ],
  "电脑模型设置尚未就绪，请刷新后重试": [
    "Desktop model settings are not ready. Refresh and try again.",
    "電腦模型設定尚未就緒，請重新整理後再試",
    "Настройки модели ещё не готовы. Обновите и повторите попытку.",
  ],
  "此队列消息包含特殊上下文，请在电脑编辑": [
    "This queued message has special context. Edit it on the computer.",
    "此佇列訊息包含特殊上下文，請在電腦編輯",
    "Это сообщение содержит особый контекст. Измените его на компьютере.",
  ],
  "队列消息已取出，引导结果未确认，请先检查电脑会话，勿重复发送": [
    "Removed from the queue; steering is unconfirmed. Check the desktop chat before resending.",
    "佇列訊息已取出，引導結果未確認，請先檢查電腦會話，勿重複傳送",
    "Сообщение извлечено из очереди, но направление не подтверждено. Проверьте беседу перед повторной отправкой.",
  ],
  "ACP Agent 未配置": [
    "ACP agent is not configured",
    "ACP Agent 未設定",
    "Агент ACP не настроен",
  ],
  "ACP 进程启动失败": [
    "Could not start the ACP process",
    "ACP 程序啟動失敗",
    "Не удалось запустить процесс ACP",
  ],
  "Agent 与会话不匹配": [
    "Agent does not match the chat",
    "Agent 與會話不符",
    "Агент не соответствует беседе",
  ],
  "Agent 接入尚未配置": [
    "Agent integration is not configured",
    "Agent 接入尚未設定",
    "Интеграция агента не настроена",
  ],
  "Agent 未应用模型或强度设置，请检查 ACP 版本": [
    "Agent did not apply model or effort settings. Check the ACP version.",
    "Agent 未套用模型或強度設定，請檢查 ACP 版本",
    "Агент не применил модель или усилие. Проверьте версию ACP.",
  ],
  "Agent 未返回模型目录，请更新 CLI 后重试": [
    "Agent returned no model catalog. Update the CLI and retry.",
    "Agent 未傳回模型目錄，請更新 CLI 後重試",
    "Агент не вернул список моделей. Обновите CLI и повторите попытку.",
  ],
  "Agent 未配置": ["Agent is not configured", "Agent 未設定", "Агент не настроен"],
  "Agent 没有提供单次允许选项": [
    "Agent does not offer a one-time approval option",
    "Agent 沒有提供單次允許選項",
    "Агент не предлагает разрешение на один раз",
  ],
  "Agent 进程退出；检查登录或网络后重试。": [
    "Agent process exited. Check sign-in or network, then retry.",
    "Agent 程序結束；檢查登入或網路後重試。",
    "Процесс агента завершился. Проверьте вход или сеть и повторите попытку.",
  ],
  "Codex 未返回会话标识": [
    "Codex returned no chat identifier",
    "Codex 未傳回會話識別碼",
    "Codex не вернул идентификатор беседы",
  ],
  "OpenCode 地址无效": [
    "Invalid OpenCode URL",
    "OpenCode 位址無效",
    "Недопустимый адрес OpenCode",
  ],
  "OpenCode 实时事件接口不可用": [
    "OpenCode live events are unavailable",
    "OpenCode 即時事件介面無法使用",
    "События OpenCode в реальном времени недоступны",
  ],
  "OpenCode 密码环境变量未配置": [
    "OpenCode password environment variable is not configured",
    "OpenCode 密碼環境變數未設定",
    "Переменная окружения с паролем OpenCode не настроена",
  ],
  "OpenCode 执行失败，请检查工作站的模型或登录配置": [
    "OpenCode failed. Check model settings or workstation sign-in.",
    "OpenCode 執行失敗，請檢查工作站的模型或登入設定",
    "Ошибка OpenCode. Проверьте модель или вход на рабочей станции.",
  ],
  "OpenCode 未返回会话 ID": [
    "OpenCode returned no chat ID",
    "OpenCode 未傳回會話 ID",
    "OpenCode не вернул ID беседы",
  ],
  "OpenCode 未连接，请检查 server 地址或登录状态": [
    "OpenCode is disconnected. Check the server URL or sign-in.",
    "OpenCode 未連線，請檢查 server 位址或登入狀態",
    "OpenCode отключён. Проверьте адрес сервера или вход.",
  ],
  "OpenCode 模型标识无效": [
    "Invalid OpenCode model identifier",
    "OpenCode 模型識別碼無效",
    "Недопустимый идентификатор модели OpenCode",
  ],
  "OpenCode 请求失败（HTTP {0}），请检查登录、版本与会话状态": [
    "OpenCode request failed (HTTP {0}). Check sign-in, version and chat status.",
    "OpenCode 請求失敗（HTTP {0}），請檢查登入、版本與會話狀態",
    "Ошибка запроса OpenCode (HTTP {0}). Проверьте вход, версию и состояние беседы.",
  ],
  "WorkBuddy 历史超过首次同步上限，请联系管理员": [
    "WorkBuddy history exceeds the initial sync limit. Contact your administrator.",
    "WorkBuddy 歷史超過首次同步上限，請聯絡管理員",
    "История WorkBuddy превышает лимит начальной синхронизации. Обратитесь к администратору.",
  ],
  "WorkBuddy 响应格式不支持": [
    "Unsupported WorkBuddy response format",
    "WorkBuddy 回應格式不支援",
    "Неподдерживаемый формат ответа WorkBuddy",
  ],
  "WorkBuddy 回调须为 HTTPS /v1/agents/workbuddy/callback": [
    "WorkBuddy callback must use HTTPS /v1/agents/workbuddy/callback",
    "WorkBuddy 回呼須為 HTTPS /v1/agents/workbuddy/callback",
    "Обратный вызов WorkBuddy должен использовать HTTPS /v1/agents/workbuddy/callback",
  ],
  "WorkBuddy 官方本地接口对应账号的一个本地助理，请打开已有本地助理会话": [
    "WorkBuddy provides one Local Assistant per account. Open its existing chat.",
    "WorkBuddy 官方本機介面對應帳號的一個本機助理，請開啟已有本機助理會話",
    "WorkBuddy предоставляет одного локального помощника на учётную запись. Откройте его существующую беседу.",
  ],
  "WorkBuddy 授权响应无效": [
    "Invalid WorkBuddy authorization response",
    "WorkBuddy 授權回應無效",
    "Недопустимый ответ авторизации WorkBuddy",
  ],
  "WorkBuddy 授权失败，请检查应用与授权范围": [
    "WorkBuddy authorization failed. Check the app and permission scopes.",
    "WorkBuddy 授權失敗，請檢查應用程式與授權範圍",
    "Авторизация WorkBuddy не удалась. Проверьте приложение и разрешения.",
  ],
  "WorkBuddy 授权已到期，请重新授权": [
    "WorkBuddy authorization expired. Authorize again.",
    "WorkBuddy 授權已到期，請重新授權",
    "Срок авторизации WorkBuddy истёк. Авторизуйтесь повторно.",
  ],
  "WorkBuddy 授权服务未连接": [
    "WorkBuddy authorization service is unavailable",
    "WorkBuddy 授權服務未連線",
    "Сервис авторизации WorkBuddy недоступен",
  ],
  "WorkBuddy 授权账号已变更，请重新授权": [
    "WorkBuddy account changed. Authorize again.",
    "WorkBuddy 授權帳號已變更，請重新授權",
    "Учётная запись WorkBuddy изменилась. Авторизуйтесь повторно.",
  ],
  "WorkBuddy 未完成请求，请检查电脑在线状态或授权范围": [
    "WorkBuddy did not complete the request. Check whether the computer is online and permissions are sufficient.",
    "WorkBuddy 未完成請求，請檢查電腦連線狀態或授權範圍",
    "WorkBuddy не выполнил запрос. Проверьте подключение компьютера и разрешения.",
  ],
  "WorkBuddy 未授予本地助理读写权限": [
    "WorkBuddy did not grant Local Assistant read/write permission",
    "WorkBuddy 未授予本機助理讀寫權限",
    "WorkBuddy не предоставил разрешение на чтение и запись для локального помощника",
  ],
  "WorkBuddy 未确认接收，请检查电脑后再操作，避免重复发送": [
    "WorkBuddy did not confirm receipt. Check the computer before retrying to avoid duplicates.",
    "WorkBuddy 未確認接收，請檢查電腦後再操作，避免重複傳送",
    "WorkBuddy не подтвердил получение. Проверьте компьютер перед повтором, чтобы избежать дубликатов.",
  ],
  "WorkBuddy 未确认消息 ID，请检查电脑后再发送": [
    "WorkBuddy did not confirm the message ID. Check the computer before sending again.",
    "WorkBuddy 未確認訊息 ID，請檢查電腦後再傳送",
    "WorkBuddy не подтвердил ID сообщения. Проверьте компьютер перед повторной отправкой.",
  ],
  "WorkBuddy 未配置": [
    "WorkBuddy is not configured",
    "WorkBuddy 未設定",
    "WorkBuddy не настроен",
  ],
  "WorkBuddy 本地助理接口未提供停止操作，请在电脑处理": [
    "WorkBuddy does not support stopping tasks here. Use the computer.",
    "WorkBuddy 本機助理介面未提供停止操作，請在電腦處理",
    "WorkBuddy не поддерживает остановку задач здесь. Используйте компьютер.",
  ],
  "WorkBuddy 本地助理的工具审批请在电脑处理": [
    "Approve WorkBuddy tools on the computer",
    "WorkBuddy 本機助理的工具審批請在電腦處理",
    "Подтвердите инструменты WorkBuddy на компьютере",
  ],
  "WorkBuddy 请求失败（HTTP {0}），请检查授权或电脑在线状态": [
    "WorkBuddy request failed (HTTP {0}). Check authorization or whether the computer is online.",
    "WorkBuddy 請求失敗（HTTP {0}），請檢查授權或電腦連線狀態",
    "Ошибка запроса WorkBuddy (HTTP {0}). Проверьте авторизацию и подключение компьютера.",
  ],
  "{0} ACP 初始化失败，请检查版本或登录": [
    "{0} ACP initialization failed. Check the version or sign-in.",
    "{0} ACP 初始化失敗，請檢查版本或登入",
    "Не удалось инициализировать ACP {0}. Проверьте версию или вход.",
  ],
  "{0} 启动失败，请在工作站安装并登录 CLI": [
    "{0} failed to start. Install the CLI and sign in on the workstation.",
    "{0} 啟動失敗，請在工作站安裝並登入 CLI",
    "Не удалось запустить {0}. Установите CLI и войдите на рабочей станции.",
  ],
  "{0} 执行失败，请检查登录、网络或模型配置": [
    "{0} failed. Check sign-in, network or model settings.",
    "{0} 執行失敗，請檢查登入、網路或模型設定",
    "Ошибка {0}. Проверьте вход, сеть или настройки модели.",
  ],
  "{0} 接口响应超时": [
    "{0} response timed out",
    "{0} 介面回應逾時",
    "Время ожидания ответа {0} истекло",
  ],
  "{0} 进程已退出，请检查工作站登录状态": [
    "{0} process exited. Check workstation sign-in.",
    "{0} 程序已結束，請檢查工作站登入狀態",
    "Процесс {0} завершился. Проверьте вход на рабочей станции.",
  ],
  "{0} 连接已断开，请检查 CLI 登录状态": [
    "{0} disconnected. Check CLI sign-in.",
    "{0} 連線已中斷，請檢查 CLI 登入狀態",
    "Соединение с {0} разорвано. Проверьте вход через CLI.",
  ],
  "一次发送的图片总计不能超过 8 MiB": [
    "Images in one message cannot exceed 8 MiB total",
    "一次傳送的圖片總計不能超過 8 MiB",
    "Общий размер изображений в сообщении не может превышать 8 МиБ",
  ],
  "一次最多 6 个附件": [
    "Up to 6 attachments at a time",
    "一次最多 6 個附件",
    "До 6 вложений за раз",
  ],
  "一次最多发送 6 个附件": [
    "Send up to 6 attachments at a time",
    "一次最多傳送 6 個附件",
    "Отправляйте до 6 вложений за раз",
  ],
  "不支持此附件链接": [
    "Unsupported attachment link",
    "不支援此附件連結",
    "Ссылка на вложение не поддерживается",
  ],
  "不支持的输入操作": [
    "Unsupported input operation",
    "不支援的輸入操作",
    "Операция ввода не поддерживается",
  ],
  "会话不存在": ["Chat does not exist", "會話不存在", "Беседа не существует"],
  "会话与项目不匹配": [
    "Chat does not match the project",
    "會話與專案不符",
    "Беседа не соответствует проекту",
  ],
  "会话图片": ["Chat image", "會話圖片", "Изображение беседы"],
  "会话当前没有活动执行": [
    "No task is currently running in this chat",
    "會話目前沒有執行中的任務",
    "В этой беседе нет выполняющейся задачи",
  ],
  "会话附件不可用": [
    "Chat attachment is unavailable",
    "會話附件無法使用",
    "Вложение беседы недоступно",
  ],
  "创建会话失败，请检查 CLI 登录状态": [
    "Could not create chat. Check CLI sign-in.",
    "建立會話失敗，請檢查 CLI 登入狀態",
    "Не удалось создать беседу. Проверьте вход через CLI.",
  ],
  "历史读取失败": [
    "Could not read history",
    "歷史讀取失敗",
    "Не удалось прочитать историю",
  ],
  "原会话恢复失败，请检查 Agent 登录与会话 ID": [
    "Could not restore the original chat. Check agent sign-in and chat ID.",
    "原會話恢復失敗，請檢查 Agent 登入與會話 ID",
    "Не удалось восстановить исходную беседу. Проверьте вход агента и ID беседы.",
  ],
  "原会话未在电脑 Codex 中打开，请先在电脑打开该会话": [
    "Open the original chat in Codex on your computer first.",
    "原會話未在電腦 Codex 中開啟，請先在電腦開啟該會話",
    "Сначала откройте исходную беседу в Codex на компьютере.",
  ],
  "图片不可用": ["Image is unavailable", "圖片無法使用", "Изображение недоступно"],
  "图片只支持 JPEG、PNG 和 GIF，请转换后重试": [
    "Images must be JPEG, PNG or GIF. Convert the image and retry.",
    "圖片只支援 JPEG、PNG 和 GIF，請轉換後重試",
    "Изображения должны быть в формате JPEG, PNG или GIF. Преобразуйте файл и повторите попытку.",
  ],
  "图片暂不可用：{0}": [
    "Image unavailable: {0}",
    "圖片暫時無法使用：{0}",
    "Изображение недоступно: {0}",
  ],
  "图片格式无效或尺寸过大，请压缩后重试": [
    "Invalid image format or dimensions too large. Compress the image and retry.",
    "圖片格式無效或尺寸過大，請壓縮後重試",
    "Неверный формат или слишком большие размеры изображения. Сожмите его и повторите попытку.",
  ],
  "图片链接已隐藏敏感凭据": [
    "Image link hidden because it contains credentials",
    "圖片連結已隱藏敏感憑證",
    "Ссылка на изображение скрыта: она содержит учётные данные",
  ],
  "审批会话已断开": [
    "Approval chat disconnected",
    "審批會話已中斷",
    "Беседа подтверждения отключена",
  ],
  "审批已失效或不属于当前会话": [
    "Approval expired or belongs to another chat",
    "審批已失效或不屬於目前會話",
    "Подтверждение истекло или относится к другой беседе",
  ],
  "审批已过期": ["Approval expired", "審批已過期", "Срок подтверждения истёк"],
  "工作站尚未启用本机语音转写": [
    "Local voice transcription is not enabled on the workstation",
    "工作站尚未啟用本機語音轉寫",
    "На рабочей станции не включено локальное распознавание речи",
  ],
  "工作站未启用会话输入": [
    "Chat input is not enabled on the workstation",
    "工作站未啟用會話輸入",
    "На рабочей станции не включён ввод сообщений",
  ],
  "工作站未启用附件读取": [
    "Attachment reading is not enabled on the workstation",
    "工作站未啟用附件讀取",
    "На рабочей станции не включено чтение вложений",
  ],
  "工作站未启用附件输入": [
    "Attachment input is not enabled on the workstation",
    "工作站未啟用附件輸入",
    "На рабочей станции не включён ввод вложений",
  ],
  "工作站未启用项目操作": [
    "Project operations are not enabled on the workstation",
    "工作站未啟用專案操作",
    "На рабочей станции не включены операции с проектами",
  ],
  "工作站未授权项目": [
    "Project is not authorized on the workstation",
    "工作站未授權專案",
    "Проект не авторизован на рабочей станции",
  ],
  "工作站未配置附件读取工具": [
    "Attachment reader is not configured on the workstation",
    "工作站未設定附件讀取工具",
    "На рабочей станции не настроено чтение вложений",
  ],
  "已发送附件保留在会话中": [
    "Sent attachments remain in the chat",
    "已傳送附件保留在會話中",
    "Отправленные вложения остаются в беседе",
  ],
  "已达到同时打开 ACP 会话上限": [
    "Maximum number of open ACP chats reached",
    "已達到同時開啟 ACP 會話上限",
    "Достигнут лимит открытых бесед ACP",
  ],
  "已达到同时打开会话上限": [
    "Maximum number of open chats reached",
    "已達到同時開啟會話上限",
    "Достигнут лимит открытых бесед",
  ],
  "当前 ACP Agent 未声明图片输入支持": [
    "This ACP agent does not advertise image input support",
    "目前 ACP Agent 未聲明圖片輸入支援",
    "Этот агент ACP не заявляет поддержку ввода изображений",
  ],
  "当前 Agent 接口未提供图片输入": [
    "This agent does not support image input",
    "目前 Agent 介面未提供圖片輸入",
    "Этот агент не поддерживает ввод изображений",
  ],
  "当前 Agent 版本不支持恢复原会话": [
    "This agent version cannot restore original chats",
    "目前 Agent 版本不支援恢復原會話",
    "Эта версия агента не поддерживает восстановление исходных бесед",
  ],
  "当前模型不支持该选择，请刷新模型与强度选项": [
    "This model does not support the selection. Refresh model and effort choices.",
    "目前模型不支援該選擇，請重新整理模型與強度選項",
    "Модель не поддерживает этот выбор. Обновите список моделей и усилий.",
  ],
  "录音最长 60 秒": [
    "Recordings can be up to 60 seconds",
    "錄音最長 60 秒",
    "Максимальная длительность записи — 60 секунд",
  ],
  "录音需为最长 60 秒的单声道 16kHz PCM WAV": [
    "Recording must be mono 16 kHz PCM WAV, up to 60 seconds",
    "錄音需為最長 60 秒的單聲道 16kHz PCM WAV",
    "Запись должна быть моно PCM WAV 16 кГц, не более 60 секунд",
  ],
  "录音需先转为文字": [
    "Convert the recording to text first",
    "錄音需先轉為文字",
    "Сначала преобразуйте запись в текст",
  ],
  "待发送附件过多，请移除附件后重试": [
    "Too many pending attachments. Remove some and retry.",
    "待傳送附件過多，請移除附件後重試",
    "Слишком много вложений ожидают отправки. Удалите часть и повторите попытку.",
  ],
  "所选模型不支持图片输入": [
    "The selected model does not support images",
    "所選模型不支援圖片輸入",
    "Выбранная модель не поддерживает изображения",
  ],
  "所选模型不支持该 effort": [
    "The selected model does not support this effort",
    "所選模型不支援此 effort",
    "Выбранная модель не поддерживает это усилие",
  ],
  "授权状态无效、过期或已使用": [
    "Authorization state is invalid, expired or already used",
    "授權狀態無效、過期或已使用",
    "Состояние авторизации недопустимо, истекло или уже использовано",
  ],
  "授权请求过多，请稍后重试": [
    "Too many authorization requests. Try again later.",
    "授權請求過多，請稍後重試",
    "Слишком много запросов авторизации. Повторите позже.",
  ],
  "授权账号已变化，请重新连接": [
    "Authorized account changed. Reconnect.",
    "授權帳號已變更，請重新連線",
    "Авторизованная учётная запись изменилась. Подключитесь повторно.",
  ],
  "操作结果尚未确认，请检查会话后再发送": [
    "Operation result is unconfirmed. Check the chat before sending again.",
    "操作結果尚未確認，請檢查會話後再傳送",
    "Результат операции не подтверждён. Проверьте беседу перед повторной отправкой.",
  ],
  "无效审批决定": [
    "Invalid approval decision",
    "無效審批決定",
    "Недопустимое решение подтверждения",
  ],
  "无效文件类型": ["Invalid file type", "無效檔案類型", "Недопустимый тип файла"],
  "无效附件分块": [
    "Invalid attachment chunk",
    "無效附件區塊",
    "Недопустимый блок вложения",
  ],
  "无效附件类型": ["Invalid attachment type", "無效附件類型", "Недопустимый тип вложения"],
  "无效附件读取范围": [
    "Invalid attachment read range",
    "無效附件讀取範圍",
    "Недопустимый диапазон чтения вложения",
  ],
  "旧会话尚未退出，请重试": [
    "Previous chat has not exited yet. Try again.",
    "舊會話尚未結束，請重試",
    "Предыдущая беседа ещё не завершена. Повторите попытку.",
  ],
  "未知 Agent": ["Unknown agent", "未知 Agent", "Неизвестный агент"],
  "未知请求": ["Unknown request", "未知請求", "Неизвестный запрос"],
  "本地助理": ["Local Assistant", "本機助理", "Локальный помощник"],
  "本机语音转写失败，请检查工作站语音服务后重试": [
    "Local transcription failed. Check the workstation speech service and retry.",
    "本機語音轉寫失敗，請檢查工作站語音服務後重試",
    "Локальное распознавание не удалось. Проверьте речевой сервис рабочей станции и повторите попытку.",
  ],
  "本机语音转写失败，请重试": [
    "Local transcription failed. Try again.",
    "本機語音轉寫失敗，請重試",
    "Локальное распознавание не удалось. Повторите попытку.",
  ],
  "模型不在工作站目录中，请刷新后重新选择": [
    "Model is not in the workstation catalog. Refresh and choose again.",
    "模型不在工作站目錄中，請重新整理後重新選擇",
    "Модели нет в каталоге рабочей станции. Обновите каталог и выберите повторно.",
  ],
  "模型目录读取失败": [
    "Could not read model catalog",
    "模型目錄讀取失敗",
    "Не удалось прочитать каталог моделей",
  ],
  "模型目录读取失败，请检查 CLI 登录状态": [
    "Could not read model catalog. Check CLI sign-in.",
    "模型目錄讀取失敗，請檢查 CLI 登入狀態",
    "Не удалось прочитать каталог моделей. Проверьте вход через CLI.",
  ],
  "正在执行，模型选择将在下一次发送时生效": [
    "A task is running. Model selection applies to the next message.",
    "正在執行，模型選擇將在下一次傳送時生效",
    "Задача выполняется. Выбор модели применится к следующему сообщению.",
  ],
  "正在执行，请等待或停止当前任务": [
    "A task is running. Wait or stop the current task.",
    "正在執行，請等待或停止目前任務",
    "Задача выполняется. Дождитесь завершения или остановите её.",
  ],
  "此 Agent 使用工作站 CLI 登录": [
    "This agent uses workstation CLI sign-in",
    "此 Agent 使用工作站 CLI 登入",
    "Этот агент использует вход через CLI рабочей станции",
  ],
  "没有识别到文字，请靠近麦克风重试": [
    "No speech recognized. Move closer to the microphone and retry.",
    "沒有辨識到文字，請靠近麥克風重試",
    "Речь не распознана. Поднесите микрофон ближе и повторите попытку.",
  ],
  "消息为空或超过 64 KiB": [
    "Message is empty or exceeds 64 KiB",
    "訊息為空或超過 64 KiB",
    "Сообщение пустое или превышает 64 КиБ",
  ],
  "用户拒绝": ["Denied by user", "使用者拒絕", "Отклонено пользователем"],
  "电脑 Codex 拒绝了操作；请检查原会话状态": [
    "Desktop Codex rejected the operation. Check the original chat status.",
    "電腦 Codex 拒絕了操作；請檢查原會話狀態",
    "Codex на компьютере отклонил операцию. Проверьте исходную беседу.",
  ],
  "电脑 Codex 接口版本不支持": [
    "Unsupported desktop Codex API version",
    "電腦 Codex 介面版本不支援",
    "Версия API Codex на компьютере не поддерживается",
  ],
  "电脑 Codex 未确认接收；请检查原会话，勿重复发送": [
    "Desktop Codex did not confirm receipt. Check the original chat; do not send duplicates.",
    "電腦 Codex 未確認接收；請檢查原會話，勿重複傳送",
    "Codex на компьютере не подтвердил получение. Проверьте исходную беседу; не отправляйте повторно.",
  ],
  "电脑 Codex 未连接": [
    "Desktop Codex is disconnected",
    "電腦 Codex 未連線",
    "Codex на компьютере отключён",
  ],
  "电脑 Codex 未连接，请启动桌面端并检查工作站桥接配置": [
    "Desktop Codex is disconnected. Start it and check the workstation bridge configuration.",
    "電腦 Codex 未連線，請啟動桌面端並檢查工作站橋接設定",
    "Codex на компьютере отключён. Запустите его и проверьте настройки моста рабочей станции.",
  ],
  "电脑 Codex 桥接未配置": [
    "Desktop Codex bridge is not configured",
    "電腦 Codex 橋接未設定",
    "Мост Codex на компьютере не настроен",
  ],
  "电脑 Codex 请求未确认，请检查原会话后再发送": [
    "Desktop Codex did not confirm the request. Check the original chat before sending again.",
    "電腦 Codex 請求未確認，請檢查原會話後再傳送",
    "Codex на компьютере не подтвердил запрос. Проверьте исходную беседу перед повторной отправкой.",
  ],
  "电脑 WorkBuddy 本地助理离线": [
    "WorkBuddy Local Assistant is offline on the computer",
    "電腦 WorkBuddy 本機助理離線",
    "Локальный помощник WorkBuddy на компьютере не в сети",
  ],
  "电脑会话与授权项目不匹配": [
    "Desktop chat does not match the authorized project",
    "電腦會話與授權專案不符",
    "Беседа на компьютере не соответствует авторизованному проекту",
  ],
  "电脑会话执行端已变更，请重新连接": [
    "Desktop execution endpoint changed. Reconnect.",
    "電腦會話執行端已變更，請重新連線",
    "Точка выполнения на компьютере изменилась. Подключитесь повторно.",
  ],
  "电脑原会话不可用": [
    "Original desktop chat is unavailable",
    "電腦原會話無法使用",
    "Исходная беседа на компьютере недоступна",
  ],
  "电脑桥接不支持模型或附件输入": [
    "Desktop bridge does not support model or attachment input",
    "電腦橋接不支援模型或附件輸入",
    "Мост компьютера не поддерживает ввод моделей или вложений",
  ],
  "目录读取失败": [
    "Could not read conversation outline",
    "目錄讀取失敗",
    "Не удалось прочитать оглавление беседы",
  ],
  "缺少项目目录": [
    "Project directory is missing",
    "缺少專案目錄",
    "Каталог проекта не указан",
  ],
  "网页图片": ["Web image", "網頁圖片", "Изображение из интернета"],
  "该会话不属于当前授权的 WorkBuddy 账号": [
    "This chat does not belong to the authorized WorkBuddy account",
    "該會話不屬於目前授權的 WorkBuddy 帳號",
    "Беседа не принадлежит авторизованной учётной записи WorkBuddy",
  ],
  "该记录已不可用": [
    "This entry is no longer available",
    "該紀錄已無法使用",
    "Эта запись больше недоступна",
  ],
  "语音服务正在转写，请稍后重试": [
    "Speech service is transcribing. Try again later.",
    "語音服務正在轉寫，請稍後重試",
    "Речевой сервис выполняет распознавание. Повторите позже.",
  ],
  "语音服务返回无效结果": [
    "Speech service returned an invalid result",
    "語音服務傳回無效結果",
    "Речевой сервис вернул недопустимый результат",
  ],
  "语音输出过大": [
    "Speech output is too large",
    "語音輸出過大",
    "Результат распознавания слишком большой",
  ],
  "请先授权 WorkBuddy 本地助理": [
    "Authorize WorkBuddy Local Assistant first",
    "請先授權 WorkBuddy 本機助理",
    "Сначала авторизуйте локального помощника WorkBuddy",
  ],
  "请先选择继续此会话": [
    "Select Continue for this chat first",
    "請先選擇繼續此會話",
    "Сначала выберите продолжение этой беседы",
  ],
  "请在工作站安装并登录 Agent CLI": [
    "Install the agent CLI and sign in on the workstation",
    "請在工作站安裝並登入 Agent CLI",
    "Установите CLI агента и войдите на рабочей станции",
  ],
  "请在工作站安装并登录 {0} CLI": [
    "Install {0} CLI and sign in on the workstation",
    "請在工作站安裝並登入 {0} CLI",
    "Установите {0} CLI и войдите на рабочей станции",
  ],
  "请在工作站配置 OpenCode server 地址": [
    "Configure the OpenCode server URL on the workstation",
    "請在工作站設定 OpenCode server 位址",
    "Настройте адрес сервера OpenCode на рабочей станции",
  ],
  "请在工作站配置 WorkBuddy 开放平台应用、密钥环境变量及 HTTPS 回调地址": [
    "Configure the WorkBuddy platform app, secret environment variable and HTTPS callback URL on the workstation.",
    "請在工作站設定 WorkBuddy 開放平台應用程式、金鑰環境變數及 HTTPS 回呼位址",
    "Настройте приложение WorkBuddy, переменную окружения с секретом и HTTPS-адрес обратного вызова на рабочей станции.",
  ],
  "请在电脑 Codex 中处理停止或工具审批": [
    "Handle stopping or tool approval in desktop Codex",
    "請在電腦 Codex 中處理停止或工具審批",
    "Остановите задачу или подтвердите инструмент в Codex на компьютере",
  ],
  "请在电脑完成 {0} CLI 登录并检查模型配置": [
    "Sign in to {0} CLI on the computer and check model settings",
    "請在電腦完成 {0} CLI 登入並檢查模型設定",
    "Войдите в {0} CLI на компьютере и проверьте настройки модели",
  ],
  "请在电脑完成 {0} CLI 登录，会话同步尚未成功": [
    "Sign in to {0} CLI on the computer. Chat sync is not ready.",
    "請在電腦完成 {0} CLI 登入，會話同步尚未成功",
    "Войдите в {0} CLI на компьютере. Синхронизация бесед ещё не выполнена.",
  ],
  "请求标识已用于不同操作": [
    "Request ID was already used for a different operation",
    "請求識別碼已用於不同操作",
    "ID запроса уже использован для другой операции",
  ],
  "请选择已授权项目": [
    "Choose an authorized project",
    "請選擇已授權專案",
    "Выберите авторизованный проект",
  ],
  "请选择模型后设置 effort": [
    "Select a model before setting effort",
    "請選擇模型後設定 effort",
    "Выберите модель перед настройкой усилия",
  ],
  "输入服务正在处理，请稍后重试": [
    "Input service is busy. Try again later.",
    "輸入服務正在處理，請稍後重試",
    "Сервис ввода занят. Повторите позже.",
  ],
  "重复附件": ["Duplicate attachment", "重複附件", "Дублирующееся вложение"],
  "附件与会话不匹配": [
    "Attachment does not match the chat",
    "附件與會話不符",
    "Вложение не соответствует беседе",
  ],
  "附件为空或超过大小限制": [
    "Attachment is empty or exceeds the size limit",
    "附件為空或超過大小限制",
    "Вложение пустое или превышает допустимый размер",
  ],
  "附件分块冲突": [
    "Conflicting attachment chunk",
    "附件區塊衝突",
    "Конфликт блока вложения",
  ],
  "附件分块大小无效": [
    "Invalid attachment chunk size",
    "附件區塊大小無效",
    "Недопустимый размер блока вложения",
  ],
  "附件分块顺序错误": [
    "Attachment chunks are out of order",
    "附件區塊順序錯誤",
    "Неверный порядок блоков вложения",
  ],
  "附件名称或大小无效（最多 8 MiB）": [
    "Invalid attachment name or size (up to 8 MiB)",
    "附件名稱或大小無效（最多 8 MiB）",
    "Недопустимое имя или размер вложения (до 8 МиБ)",
  ],
  "附件大小不匹配": [
    "Attachment size mismatch",
    "附件大小不符",
    "Размер вложения не совпадает",
  ],
  "附件尚未上传完成": [
    "Attachment upload is not complete",
    "附件尚未上傳完成",
    "Загрузка вложения не завершена",
  ],
  "附件已失效，请重新添加": [
    "Attachment expired. Add it again.",
    "附件已失效，請重新新增",
    "Срок вложения истёк. Добавьте его повторно.",
  ],
  "附件总计不能超过 24 MiB": [
    "Attachments cannot exceed 24 MiB total",
    "附件總計不能超過 24 MiB",
    "Общий размер вложений не может превышать 24 МиБ",
  ],
  "附件读取失败或过大": [
    "Could not read attachment or attachment is too large",
    "附件讀取失敗或過大",
    "Не удалось прочитать вложение или оно слишком большое",
  ],
  "需要录音附件": [
    "An audio recording attachment is required",
    "需要錄音附件",
    "Требуется вложение с аудиозаписью",
  ],
  "项目不在工作站授权目录内": [
    "Project is outside the workstation's authorized directories",
    "專案不在工作站授權目錄內",
    "Проект вне авторизованных каталогов рабочей станции",
  ],
  "项目目录不可用": [
    "Project directory is unavailable",
    "專案目錄無法使用",
    "Каталог проекта недоступен",
  ],
  "项目目录不存在": [
    "Project directory does not exist",
    "專案目錄不存在",
    "Каталог проекта не существует",
  ],
  "无效的消息发送方式": [
    "Invalid message delivery mode",
    "無效的訊息傳送方式",
    "Недопустимый режим отправки",
  ],
  "此 Agent 使用自身的发送方式": [
    "This agent uses its native send behavior",
    "此 Agent 使用自身的傳送方式",
    "Этот агент использует собственный режим отправки",
  ],
  "待发消息已不可用": [
    "Queued message is no longer available",
    "待傳送訊息已無法使用",
    "Сообщение в очереди больше недоступно",
  ],
  "消息已提交，请先检查会话": [
    "Message has already been submitted; check the conversation",
    "訊息已提交，請先檢查會話",
    "Сообщение уже отправлено; проверьте беседу",
  ],
  "桌面引导接口不可用": [
    "Desktop steering is unavailable",
    "桌面引導介面無法使用",
    "Управление текущей задачей недоступно",
  ],
  "没有正在执行的任务，请正常发送": [
    "No running turn to steer; send normally",
    "沒有正在執行的任務，請正常傳送",
    "Нет выполняемой задачи; отправьте обычное сообщение",
  ],
  "尚未确认中断，消息未发送": [
    "Interrupt is not confirmed; message was not sent",
    "尚未確認中斷，訊息未傳送",
    "Прерывание не подтверждено; сообщение не отправлено",
  ],
  "桌面会话项目已变更，请重新连接": [
    "Desktop project identity changed",
    "桌面會話專案已變更，請重新連線",
    "Проект беседы изменился; подключитесь заново",
  ],
  "消息队列已满": ["Message queue is full", "訊息佇列已滿", "Очередь сообщений заполнена"],
  "队列附件超过大小限制": [
    "Queued attachments exceed the size limit",
    "佇列附件超過大小限制",
    "Вложения в очереди превышают ограничение размера",
  ],
  "待发消息标识冲突": [
    "Queued message identity conflict",
    "待傳送訊息識別碼衝突",
    "Конфликт идентификатора сообщения",
  ],
  "待发消息与会话不匹配": [
    "Queued message does not match this chat",
    "待傳送訊息與會話不符",
    "Сообщение очереди не соответствует беседе",
  ],
  '加密的 Office 文件需要先在工作站解密': [
    'Unlock password-protected Office files on the workstation first.',
    '加密的 Office 檔案需要先在工作站解密',
    'Сначала снимите пароль с файла Office на рабочей станции.',
  ],
  '文档无法预览，文件可能损坏或格式不支持': [
    'Cannot preview this document. It may be damaged or unsupported.',
    '無法預覽文件，檔案可能損壞或格式不支援',
    'Не удалось открыть документ. Возможно, файл повреждён или формат не поддерживается.',
  ],
};

// Only these known backend notices may be localized in the transcript.
const backendNoticeKeys = <String>{
  "此 Claude 会话已从桌面目录移除或归档",
  "此会话保留在 Claude 桌面目录中，但本机没有对应的日志，暂时无法读取历史或从手机续聊",
  "Claude 本机会话日志暂时无法读取",
  "Agent 进程退出；检查登录或网络后重试。",
  "OpenCode 执行失败，请检查工作站的模型或登录配置",
  "{0} 执行失败，请检查登录、网络或模型配置",
  "{0} 连接已断开，请检查 CLI 登录状态",
  "图片暂不可用：{0}",
  "图片链接已隐藏敏感凭据",
};
