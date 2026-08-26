# Darling

把 AI 角色装进一个像微信一样的 H5 小世界里。

项目地址：[https://github.com/Candyman2077/Darling](https://github.com/Candyman2077/Darling)

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
- DeepSeek Agent 会在“文字回复 / 只发表情包 / 不回复”之间做严格互斥决策。
- 表情包可从角色配置读取，也可以按需抓取并缓存在 PostgreSQL。
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

### 1. 克隆项目

```powershell
git clone https://github.com/Candyman2077/Darling.git
cd Darling
```

### 2. 准备

你需要：

- Go 1.22+
- PostgreSQL 16+，或者可用的 Docker Compose
- 一个 DeepSeek API Key
- 如果想让 AI 朋友圈发图，再准备一个图片模型 API Key
- 如果想启用微信扫码登录，再准备一个微信开放平台已审核的“网站应用”

### 3. 配置 `.env`

复制示例配置：

```powershell
Copy-Item .env.example .env
```

至少填写：

```env
DATABASE_URL=postgresql://darling:darling_local@127.0.0.1:5433/darling?sslmode=disable
DEEPSEEK_API_KEY=your_deepseek_api_key
DEEPSEEK_BASE_URL=https://api.deepseek.com
DEEPSEEK_MODEL=deepseek-v4-flash
```

DeepSeek 使用兼容 OpenAI 的 `/chat/completions` 接口和 JSON Output。真实 Key 只能放在已忽略的 `.env` 或部署平台 Secret 中，不要提交到 Git。

### 微信扫码登录（可选）

默认不配置微信参数时，应用继续使用匿名浏览器身份，原有本地体验不受影响。要启用扫码登录，需要先满足以下条件：

1. 在[微信开放平台](https://open.weixin.qq.com/)创建并通过审核的“网站应用”。公众号或小程序的 AppID 不能直接替代网站应用 AppID。
2. 在该网站应用中配置“授权回调域”，并让应用可以通过该域名的 HTTPS 地址从公网访问。
3. 将完整回调地址配置到后端；地址必须属于已登记的授权回调域，路径使用 `/api/auth/wechat/callback`。

在 `.env` 中填写：

```env
WECHAT_APP_ID=your_wechat_website_app_id
WECHAT_APP_SECRET=your_wechat_website_app_secret
WECHAT_REDIRECT_URL=https://chat.example.com/api/auth/wechat/callback
```

登录流程从 `/api/auth/wechat/start` 开始，使用微信网站扫码登录的 `snsapi_login` scope，微信授权后回到 `/api/auth/wechat/callback`。`WECHAT_REDIRECT_URL` 必须是完整 HTTPS URL；`localhost`、局域网地址或未登记的临时隧道域名通常不能完成真实微信授权。

三个变量任意一个为空时，微信登录功能会保持禁用，用户仍以匿名身份使用。`WECHAT_APP_SECRET` 只能保存在后端 `.env` 或部署平台 Secret 中，不能传给前端，也不能提交到 Git。

生产环境的反向代理、CDN 和 APM 也应对 `/api/auth/wechat/callback` 跳过查询字符串访问日志，避免临时 `code` 和 `state` 进入日志。

GitHub 临时地址工作流会读取同名仓库 Secrets：

- `WECHAT_APP_ID`
- `WECHAT_APP_SECRET`
- `WECHAT_REDIRECT_URL`

Secrets 未配置时工作流仍可启动，微信登录保持禁用。由于 `trycloudflare.com` 地址每次可能变化，它通常不适合作为微信开放平台已审核的固定授权回调域。

### 4. 启动

使用 Docker Compose：

```powershell
docker compose up --build
```

或者先准备好 PostgreSQL、设置 `DATABASE_URL`，再直接运行：

```powershell
go run .
```

打开浏览器：

```text
http://localhost:8080
```

### 数据隔离

未登录用户第一次访问时，服务端会创建 PostgreSQL 用户，并通过 HttpOnly Cookie 签发随机 256-bit opaque 会话令牌；数据库只保存令牌的 SHA-256 摘要，不把稳定 `user_id` 暴露给浏览器。消息、朋友圈、点赞、评论、检查状态和表情缓存全部显式带 `user_id`；朋友圈子表还使用 `(user_id, moment_id)` 复合外键，阻止跨用户引用。

某个微信身份第一次登录时会绑定当前匿名 `user_id`，保留已有聊天和朋友圈；以后从其他浏览器扫码，会切换到这个已绑定的用户。扫码进入已存在的微信账号会直接切换到目标账号；若当前浏览器已绑定另一个微信身份，而要登录一个尚未绑定的新微信账号，请先点“退出微信登录”再扫码。系统不会自动合并两边的数据，未选中的匿名数据也不会被删除。

匿名模式下，不同浏览器配置文件或无痕会话互不共享数据。清除站点 Cookie 后会获得新身份，原数据仍保留在 PostgreSQL，但不会自动重新关联。数据库查询始终使用服务端会话解析出的 `user_id`，浏览器不能自行指定其他用户身份。

`USER_DB_MAX_USERS` 默认限制为 `1000`。达到上限后已有身份仍可使用，新身份收到 `503`。公开部署还应配置入口限流、请求配额和数据库备份。

旧版 `runtime/users/<id>/dimension.db` 不会自动导入 PostgreSQL；需要保留旧数据时，应先编写一次性迁移脚本再删除 SQLite 文件。

### Docker

`compose.yaml` 同时启动 PostgreSQL 与应用，数据库数据保存在命名卷 `postgres-data`。宿主 PostgreSQL 端口映射为 `127.0.0.1:5433`，避免占用常见的本机 5432。

Compose 会把 `WECHAT_APP_ID`、`WECHAT_APP_SECRET` 和 `WECHAT_REDIRECT_URL` 转发给应用；未设置时三个值为空，服务保持匿名模式。

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
- `stickers`：可选，自定义补充表情包。抓取到的资源会按浏览器身份缓存在 PostgreSQL。

如果你想给自己的角色手动补充表情包，可以这样写：

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

默认角色可按需抓取麻衣相关资源，并缓存在 PostgreSQL 的 `sticker_assets` 表；Agent 只选择表情类别，服务端再从当前用户可用资源中挑选 URL，模型不能注入任意表情地址。

如果你手动补充，建议使用稳定图片直链，不要用容易过期的搜索缩略图。

---

## 省 token 逻辑

Darling 不是每隔几秒就硬调 AI。

- 聊天会先缓存用户连续消息，约 10 秒后一起交给 AI。
- 朋友圈会先查 PostgreSQL；没有新的用户动态信号时不会调用 Agent。
- 点赞不需要 AI，直接走本地逻辑。
- 只有需要判断“要不要评论”或“要不要自己发朋友圈”时，才调用模型。
- 控制台会打印 `prompt_tokens`、`completion_tokens`、`total_tokens`。

---

## 小提示

- 改了 `main.go` 或 `.env` 后，需要重启后端。
- 改了 `web/app.js`、`web/style.css`、`web/index.html` 后，刷新页面即可。
- 聊天记录、朋友圈、评论、点赞和表情缓存都在 PostgreSQL 中按 `user_id` 隔离。
- 麻衣表情包会自动抓取并入库；如果你换成别的角色，可以在 `luna.yaml` 里补 `stickers`。
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
├─ agent.go                 # 三态 Agent 契约与严格校验
├─ deepseek_agent.go        # DeepSeek JSON 决策客户端
├─ postgres.go              # 连接池与 Schema
├─ postgres_storage.go      # 显式租户 CRUD
├─ main.go
├─ compose.yaml
├─ .env.example
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
