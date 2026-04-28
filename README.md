# DimensionMessenger Demo

一个本地可运行的沉浸式二次元 AI 聊天 Demo。

这个项目当前定位是 **本地原型 / Demo**：

- 先快速验证“二次元角色聊天”这件事是否好玩
- 先把角色人设、聊天体验、表情包机制跑通
- 先用最轻量的本地方案完成可玩的版本

它不是最终形态，但它已经具备了后续继续扩展成正式产品的基础。

当前版本支持：

- 类即时通讯聊天界面
- Go + Gin 后端
- SQLite 本地聊天记录存储
- 通义千问 OpenAI-Compatible API
- YAML 角色卡配置
- 角色头像 / 用户头像 URL 配置
- 按情绪发送角色表情包 URL

目前默认角色已经配置为 **樱岛麻衣** 风格。

---

## 截图

你可以把项目截图放到下面这个目录：

```text README.md
docs/screenshots/
```

推荐文件名：

```text README.md
docs/screenshots/chat-main.png
docs/screenshots/chat-sticker.png
docs/screenshots/chat-persona.png
docs/screenshots/yaml-config.png
```

当前已放入两张示例截图：

### 聊天主界面
![聊天主界面](docs/screenshots/chat-main.png)

### 人设回复示例
![人设回复示例](docs/screenshots/chat-persona.png)

如果你后面继续补图，可以继续使用这些文件名：

<!--
### 表情包触发示例
![表情包触发示例](docs/screenshots/chat-sticker.png)

### YAML 配置示例
![YAML 配置示例](docs/screenshots/yaml-config.png)
-->

---

## 功能特性

- 本地启动，打开浏览器即可聊天
- 聊天记录保存到 `dimension.db`
- 每次对话会读取最近聊天记录，保持上下文
- 角色人设写在 YAML 里，方便改角色
- 头像和表情包支持直接配置远程 URL，不需要保存在本地
- 表情包发送不再完全依赖模型，可按情绪概率自动触发

---

## 项目结构

```text README.md
.
├─ data/
│  └─ characters/
│     └─ luna.yaml
├─ docs/
│  └─ screenshots/
│     ├─ chat-main.png
│     └─ chat-persona.png
├─ web/
│  ├─ app.js
│  ├─ index.html
│  └─ style.css
├─ main.go
├─ go.mod
└─ README.md
```

---

## 环境要求

- Go 1.22+
- 一个可用的通义千问 / OpenAI-Compatible API Key

---

## 环境变量与 `.env`

项目支持从根目录的 `.env` 文件自动读取配置。

也就是说，**你现在不需要每次都手写 PowerShell 的 `$env:`**。

### 1. 在项目根目录创建 `.env`

```env README.md
AI_MID_API_KEY=your_api_key
AI_MID_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
AI_MID_MODEL=qwen-plus
AI_MID_TEMPERATURE=0.7
AI_MID_MAX_TOKENS=4096
AI_MID_TIMEOUT=120
```

如果你使用阿里云百炼 / DashScope OpenAI 兼容接口，通常这样配就可以。

### 2. 启动项目

```powershell README.md
go run .
```

### 3. 打开浏览器

```text README.md
http://localhost:8080
```

### 4. 补充说明

- 如果系统环境变量和 `.env` 同时存在，**系统环境变量优先**
- `.env` 不应该提交到 Git 仓库
- API Key 泄露后请立刻重置

---

## 如何使用

### 第一步：配置大模型

在根目录创建 `.env`，填入你自己的模型配置。

### 第二步：配置角色

编辑：

```text README.md
data/characters/luna.yaml
```

你可以修改：

- 角色名称
- 角色背景
- 性格与说话风格
- 角色规则
- 角色头像 URL
- 用户头像 URL
- 不同情绪下的表情包 URL

### 第三步：启动项目

```powershell README.md
go run .
```

### 第四步：开始聊天

浏览器打开：

```text README.md
http://localhost:8080
```

然后你就可以：

- 输入消息和角色聊天
- 观察角色是否保持人设
- 测试不同情绪下的表情包返回
- 清空聊天记录重新测试

---

## 角色配置

角色配置文件位置：

```text README.md
data/characters/luna.yaml
```

你可以在这里修改：

- 角色名称
- 背景设定
- 性格
- 说话风格
- 规则
- 角色头像 URL
- 用户头像 URL
- 表情包 URL

---

## 头像和表情包 URL 配置

当前已经支持直接在 YAML 里配置远程图片地址。

示例：

```yaml README.md
id: luna
name: 樱岛麻衣
avatar: https://your-cdn.com/mai/avatar.png
user_avatar: https://your-cdn.com/user/avatar.png

stickers:
  happy:
    - https://your-cdn.com/mai/happy_1.png
    - https://your-cdn.com/mai/happy_2.png
  shy:
    - https://your-cdn.com/mai/shy_1.png
  angry:
    - https://your-cdn.com/mai/angry_1.png
  sad:
    - https://your-cdn.com/mai/sad_1.png
  worried:
    - https://your-cdn.com/mai/worried_1.png
  jealous:
    - https://your-cdn.com/mai/jealous_1.png
  teasing:
    - https://your-cdn.com/mai/teasing_1.png
  sleepy:
    - https://your-cdn.com/mai/sleepy_1.png
```

说明：

- `avatar` 是角色头像
- `user_avatar` 是用户头像
- `stickers` 按情绪分组
- 每个情绪可以配置多张图，系统会随机选择一张

建议使用稳定的图片直链，不建议使用容易失效的搜索缩略图链接。

---

## 表情包触发逻辑

表情包触发分两层：

1. 模型主动要求发图
2. 即使模型没要求，只要当前情绪明显且该情绪配置了表情包，也会按概率自动发图

当前高频情绪：

- `happy`
- `shy`
- `worried`
- `jealous`
- `teasing`
- `excited`

中频情绪：

- `sad`
- `angry`
- `sleepy`

支持的情绪枚举：

```text README.md
neutral
happy
angry
sad
shy
teasing
worried
jealous
sleepy
excited
```

其中 `excited` 如果没有单独配置，会回退使用 `happy` 的表情包。

---

## 接口说明

### 获取角色配置

```http README.md
GET /api/character?character_id=luna
```

### 获取聊天记录

```http README.md
GET /api/messages?character_id=luna
```

### 发送消息

```http README.md
POST /api/chat/send
Content-Type: application/json
```

请求体：

```json README.md
{
  "character_id": "luna",
  "message": "喜欢你"
}
```

### 清空聊天记录

```http README.md
POST /api/messages/clear?character_id=luna
```

---

## 常见问题

### 1. 头像不显示

检查：

- `avatar` / `user_avatar` 是否是可直接访问的图片 URL
- 浏览器里单独打开图片 URL 是否能直接看到图片

### 2. 表情包不显示

检查：

- 当前情绪是否在 `stickers` 里配置了图片
- 图片 URL 是否稳定
- 是否用了搜索缩略图或防盗链地址

### 3. 表情包频率太低

当前代码已经做了概率补偿。如果你还想更高，可以继续调整 `main.go` 里的 `shouldSendSticker` 逻辑。

### 4. 想改角色

直接复制一份 YAML，修改角色人设、头像和表情包地址即可。

---

## 注意事项

- 请不要把真实 API Key 提交到 Git 仓库
- 如果 API Key 泄露，请立刻去控制台重置
- 如果使用受版权保护的角色图片，请自行注意使用范围
- 远程图片 URL 尽量使用稳定图床或自己的对象存储

---

## 当前 Demo 的定位

当前版本更像一个 **可玩的产品原型**，主要解决的是：

- 本地能不能快速跑起来
- AI 角色聊天是否有感觉
- 人设 + 上下文 + 表情包 的组合是否成立
- 用 SQLite + YAML + 单页前端是否足够支撑第一阶段验证

所以这个阶段优先级是：

1. 好玩
2. 可调试
3. 易改角色
4. 本地一键可跑

而不是：

- 大规模部署
- 复杂账号体系
- 高并发架构
- 完整商业化后台

---

## 后面会如何发展

如果继续往下做，这个项目可以自然演进成更正式的版本。

### Phase 1：完善当前 Demo

目标：把当前本地 Demo 打磨到更稳定、更像产品。

可以继续做：

- 多角色切换
- 更完善的 YAML 配置
- 更稳定的图片兜底
- 调试信息面板
- 更像聊天软件的 UI
- 关键词触发表情包 / 特殊回复

### Phase 2：升级前端

目标：从当前单页静态前端升级到更正式的前端工程。

可以继续做：

- React / TypeScript 前端
- 更细的状态管理
- 聊天列表页
- 角色选择页
- 角色编辑页
- 表情包管理页

### Phase 3：增强角色能力

目标：让角色不只是“答一句话”，而是更像有持续人格的陪伴对象。

可以继续做：

- 长期记忆
- 好感度系统
- 剧情分支
- 主动发消息
- 节日 / 早晚安 / 特殊事件互动
- 更强的人设一致性

### Phase 4：更完整的产品化

目标：从个人 Demo 走向可分享、可部署、可扩展的应用。

可以继续做：

- 多用户支持
- 账号体系
- 云端部署
- 角色市场
- 用户自定义角色
- AI 生成表情包 / 立绘
- 语音聊天

---

## 后续可扩展方向

- 多角色切换
- React / TypeScript 前端
- 主动发消息
- 长期记忆
- 剧情系统
- AI 生成角色表情包
- 语音聊天
- 多用户支持
- 角色编辑器
- 角色市场

---

## License

当前仓库未单独声明许可证，如需开源发布，建议补充 `LICENSE` 文件。
