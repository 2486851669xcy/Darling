# Darling

把 AI 角色装进一个像微信一样的 H5 小世界里。

Darling 是一个本地可运行的二次元陪伴聊天 Demo：你可以和角色聊天、发朋友圈、等她主动找你、看她点赞评论你的动态，甚至看她自己发朋友圈。它不是一个冷冰冰的问答框，而是一个会“生活”在消息列表和朋友圈里的角色。

默认角色是 **樱岛麻衣** 风格。你也可以把她换成任何你喜欢的角色。

如果你喜欢“AI 角色 + 微信式沉浸界面 + 朋友圈互动”这种方向，欢迎点个 Star，后面会继续把它做得更像真的。

---

## 现在能玩什么

- 像微信一样的 H5 界面：消息、联系人、朋友圈、我的。
- 角色聊天不是一大段灌水，会拆成多条消息慢慢发。
- 用户可以连续发多条，AI 会等一会儿再已读并统一理解，少耗 token。
- AI 可以觉得没必要回复，然后自然结束话题。
- 她会偶尔主动发消息，不需要永远等你先开口。
- 有发送/接收提示音、已读小圈圈、居中时间、打字中的 `...` 气泡。
- 你可以发文字朋友圈。
- 她会看你的朋友圈，点赞、评论，或者自己发朋友圈。
- 如果 AI 更新发生在你没打开的页面，会有顶部弹窗和红点提醒。
- 表情包支持从角色配置读取，也支持自动抓取麻衣相关表情包写进 SQLite。
- 控制台会打印 token usage，方便你知道钱花在哪。

---

## 界面预览

截图放在 `docs/screenshots/`。只需要上传用户能看到的界面截图。

| 消息 | 聊天 |
| --- | --- |
| ![消息主页](docs/screenshots/messages-home.png) | ![聊天详情](docs/screenshots/chat-detail.png) |

| 朋友圈 | 通知 |
| --- | --- |
| ![朋友圈](docs/screenshots/moments-home.png) | ![顶部提醒和红点](docs/screenshots/notification-toast.png) |

| 联系人 | 我的 |
| --- | --- |
| ![联系人](docs/screenshots/contacts-home.png) | ![我的](docs/screenshots/profile-home.png) |

| AI 朋友圈 |
| --- |
| ![AI 朋友圈互动](docs/screenshots/ai-moment.png) |

---

## 快速开始

### 1. 准备

你需要：

- Go 1.22+
- 一个 OpenAI 兼容格式的聊天模型 API Key
- 如果想让 AI 发图，再准备一个图片模型 API Key

### 2. 配置 `.env`

在项目根目录新建 `.env`：

```env
AI_MID_API_KEY=your_chat_api_key
AI_MID_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
AI_MID_MODEL=qwen-plus
AI_MID_TEMPERATURE=0.7
AI_MID_MAX_TOKENS=4096
AI_MID_TIMEOUT=120

AI_HIGH_API_KEY=your_image_api_key
AI_HIGH_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai/
AI_HIGH_MODEL=gemini-2.5-flash-image
AI_HIGH_TEMPERATURE=0.8
AI_HIGH_MAX_TOKENS=4096
AI_HIGH_TIMEOUT=300
```

只聊天的话，先配 `AI_MID_*` 就能跑。想让 AI 朋友圈带图，再配 `AI_HIGH_*`。

不要把真实 API Key 提交到 GitHub。

### 3. 启动

```powershell
go run .
```

打开浏览器：

```text
http://localhost:8080
```

---

## 怎么换成你的角色

角色配置在：

```text
data/characters/luna.yaml
```

常改这些就够了：

- `name`：角色名字。
- `avatar`：角色头像。
- `user_avatar`：你的头像。
- `relationship`：你们是什么关系。
- `background`：角色背景。
- `personality`：性格关键词。
- `speech_style`：说话风格。
- `rules`：角色必须遵守的规则。
- `stickers`：不同情绪下会发的表情包。

表情包示例：

```yaml
stickers:
  happy:
    - https://your-cdn.com/character/happy_1.png
    - https://your-cdn.com/character/happy_2.png
  shy:
    - https://your-cdn.com/character/shy_1.png
  teasing:
    - https://your-cdn.com/character/teasing_1.png
```

建议使用稳定图片直链，不要用容易过期的搜索缩略图。

---

## 省 token 逻辑

Darling 不是每隔几秒就硬调 AI。

- 聊天会先缓存用户连续消息，约 10 秒后一起交给 AI。
- 朋友圈会先查 SQLite，如果没有新的用户动态信号，就不会调用 AI。
- 点赞不需要 AI，直接走本地逻辑。
- 只有需要判断“要不要评论”或“要不要自己发朋友圈”时，才调用模型。
- 控制台会打印 `prompt_tokens`、`completion_tokens`、`total_tokens`。

---

## 小提示

- 改了 `main.go` 或 `.env` 后，需要重启后端。
- 改了 `web/app.js`、`web/style.css`、`web/index.html` 后，刷新页面即可。
- 聊天记录、朋友圈、评论、点赞、表情包缓存都存在 `dimension.db`。
- 主动消息和朋友圈检查不是固定时间触发，等一会儿才会出现。
- 如果你看到 Gin 的 trusted proxies 警告，本地运行可以忽略。

---

## 项目结构

```text
.
├─ data/
│  └─ characters/
│     └─ luna.yaml
├─ docs/
│  └─ screenshots/
├─ web/
│  ├─ app.js
│  ├─ index.html
│  └─ style.css
├─ main.go
├─ go.mod
├─ LICENSE
└─ README.md
```

---

## Roadmap

- 朋友圈图片上传。
- 多角色切换。
- 更长期的记忆。
- 好感度或事件系统。
- 语音消息。
- 更像微信的细节动画。
- 角色编辑器。

---

## License

MIT License。你可以自由使用、修改和分享这个项目；如果拿去二创，记得保留许可证声明。
