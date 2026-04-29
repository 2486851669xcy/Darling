# 已读 / 正在输入 / 朋友圈状态实现方案

## 目标

让角色更像真的在使用聊天软件，而不是接口秒回。

这次功能只做轻量状态感：

- 用户发消息后先保持未读状态，约 10 秒没有新消息后再显示“麻衣 已读”。
- AI 准备回复时，再显示“麻衣 正在输入...”。
- AI 连续发送多条消息时，保持自然的发送节奏。
- 用户在麻衣未读前可以连续发多条消息，前端先缓存，约 10 秒后一次性让 AI 读取并回复。
- 只有缓存里确实有用户消息时，才允许触发 AI 调用，避免空轮询浪费 token。
- 麻衣已读后锁住输入框，等她发完再解锁；用户打了一半的草稿要保留。
- 用户停留在朋友圈页时，AI 检查朋友圈可以短暂显示“麻衣刚刚看了你的朋友圈”。
- 如果 AI 点赞或评论，仍然按现有逻辑刷新朋友圈、显示红点和弹窗。

这个功能主要改前端，不需要新增数据库表。

---

## 涉及文件

```text
web/index.html
web/app.js
web/style.css
```

后端暂时不用改。

---

## 页面结构

### 1. 聊天页增加已读状态

在 `web/index.html` 的聊天区域里，放在 `typingIndicator` 附近：

```html
<div id="readStatus" class="read-status hidden"></div>
```

建议位置：

```html
<main id="messageList" class="message-list"></main>

<div id="readStatus" class="read-status hidden"></div>

<div id="typingIndicator" class="typing-indicator hidden">
  樱岛麻衣 正在输入...
</div>
```

### 2. 朋友圈页增加轻量状态

在朋友圈输入框和列表之间增加：

```html
<div id="momentStatus" class="moment-status hidden"></div>
```

建议位置：

```html
<section class="moment-composer">
  ...
</section>

<div id="momentStatus" class="moment-status hidden"></div>

<section id="momentsList" class="moments-list"></section>
```

---

## 前端状态

在 `web/app.js` 顶部增加 DOM 引用：

```js
const readStatus = document.getElementById('readStatus');
const momentStatus = document.getElementById('momentStatus');
```

在 `state` 里增加计时器：

```js
readStatusTimer: null,
momentStatusTimer: null,
pendingUserMessages: [],
pendingSendTimer: null,
isWaitingForAI: false,
unreadBatchDelay: 10 * 1000,
```

字段含义：

- `pendingUserMessages`：麻衣还没已读前，用户连续发送的消息缓存。
- `pendingSendTimer`：10 秒未读计时器。
- `isWaitingForAI`：麻衣已读后到回复完成前为 `true`，这段时间锁输入框。
- `unreadBatchDelay`：用户停顿多久后，麻衣才已读并调用 AI，默认 10 秒。

---

## 工具函数

新增几个小函数：

```js
function randomBetween(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

function showReadStatus(text, duration = 1800) {
  window.clearTimeout(state.readStatusTimer);
  readStatus.textContent = text;
  readStatus.classList.remove('hidden');

  state.readStatusTimer = window.setTimeout(() => {
    readStatus.classList.add('hidden');
  }, duration);
}

function hideReadStatus() {
  window.clearTimeout(state.readStatusTimer);
  readStatus.classList.add('hidden');
}

function showMomentStatus(text, duration = 2200) {
  window.clearTimeout(state.momentStatusTimer);
  momentStatus.textContent = text;
  momentStatus.classList.remove('hidden');

  state.momentStatusTimer = window.setTimeout(() => {
    momentStatus.classList.add('hidden');
  }, duration);
}

function hideMomentStatus() {
  window.clearTimeout(state.momentStatusTimer);
  momentStatus.classList.add('hidden');
}
```

如果项目里已经有类似随机延迟函数，可以复用，不需要重复加。

还需要一个通用等待函数：

```js
function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
```

---

## 聊天发送流程：未读缓存队列

修改 `handleSend()`。

当前用户发送消息后，不要立刻调用 `sendMessage()`。先把消息显示在界面里，并放进缓存。

1. 用户消息先显示到聊天框。
2. 播放发送音。
3. 把文本放入 `pendingUserMessages`。
4. 启动或刷新 10 秒计时器。
5. 10 秒内如果用户继续发消息，就继续缓存，并重新计时。
6. 10 秒到了，如果缓存里有消息，麻衣才已读。
7. 麻衣已读后锁输入框，调用一次 AI。
8. AI 发完后解锁输入框，并保留用户之前打了一半的草稿。

### handleSend 伪代码

```js
async function handleSend() {
  if (state.isWaitingForAI) return;

  const text = messageInput.value.trim();
  if (!text) return;

  appendMessage({
    sender: 'user',
    type: 'text',
    content: text,
    created_at: new Date().toISOString(),
  });

  playMessageSound('send');
  state.pendingUserMessages.push(text);
  messageInput.value = '';

  scheduleFlushUserMessages();
}
```

这里不能调用 `setLoading(true)`，因为麻衣还没已读，用户应该还能继续发。

---

## 10 秒检查：必须有缓存才调用 AI

不要做成固定每 10 秒调用一次 AI。

正确逻辑是：

- 用户发消息后，才启动 10 秒计时器。
- 每次用户继续发消息，都重置这个计时器。
- 计时器触发时，先检查 `pendingUserMessages.length`。
- 如果没有缓存消息，直接返回，不调用 AI。
- 如果已有 AI 正在回复，也直接返回，不调用 AI。

```js
function scheduleFlushUserMessages() {
  window.clearTimeout(state.pendingSendTimer);

  if (state.pendingUserMessages.length === 0 || state.isWaitingForAI) {
    return;
  }

  state.pendingSendTimer = window.setTimeout(() => {
    flushUserMessages();
  }, state.unreadBatchDelay);
}
```

真正调用 AI 前也要再检查一次，防止计时器边界情况：

```js
async function flushUserMessages() {
  if (state.isWaitingForAI || state.pendingUserMessages.length === 0) {
    return;
  }

  const batch = [...state.pendingUserMessages];
  state.pendingUserMessages = [];
  state.isWaitingForAI = true;

  const draftText = messageInput.value;
  const mergedText = batch.join('\n\n');

  showReadStatus(`${state.character.name} 已读`);
  setLoading(true);

  try {
    await sleep(randomBetween(700, 1300));
    hideReadStatus();
    typingIndicator.classList.remove('hidden');

    const result = await sendMessage(mergedText);
    const messages = Array.isArray(result.messages) ? result.messages : [];

    await appendMessagesSequentially(messages, {
      playReceiveSound: messages.length > 0,
    });

    scheduleProactiveMessage();
  } catch (error) {
    appendMessage({
      sender: 'character',
      type: 'text',
      content: `出错了：${error.message}`,
    });
  } finally {
    state.isWaitingForAI = false;
    setLoading(false);
    hideReadStatus();
    typingIndicator.classList.add('hidden');

    if (messageInput.value === '') {
      messageInput.value = draftText;
    }
    messageInput.focus();

    if (state.pendingUserMessages.length > 0) {
      scheduleFlushUserMessages();
    }
  }
}
```

这样可以保证：

- 用户发 1 条消息，最多调用 1 次 AI。
- 用户 10 秒内连续发 5 条消息，仍然只调用 1 次 AI。
- 用户不发消息时，不会因为定时器空转调用 AI。
- AI 回复期间不会重复调用 AI。

---

## 已读和输入框锁定

锁输入框的时机很重要。

不要在用户刚发消息时锁，因为这时麻衣还没已读，用户应该可以补充多条消息。

应该在 `flushUserMessages()` 开始后锁：

```text
麻衣 已读
```

对应逻辑：

```js
showReadStatus(`${state.character.name} 已读`);
setLoading(true);
```

这时如果用户正在输入框里打字，`messageInput.value` 里可能有草稿。需要在锁定前保存：

```js
const draftText = messageInput.value;
```

AI 回复结束后，如果输入框被清空，就恢复草稿：

```js
if (messageInput.value === '') {
  messageInput.value = draftText;
}
```

注意：不要恢复已经被用户成功发送出去的内容，只恢复“已读瞬间正在输入框里、还没发送”的草稿。

---

## AI 连续回复节奏

项目里已有 `appendMessagesSequentially()`，可以在这里优化每条消息之间的间隔。

建议规则：

- 第一条回复前：600 到 1200 毫秒。
- 后续每条气泡之间：700 到 1500 毫秒。
- 如果是图片或表情包，可以稍微快一点。

示例：

```js
const delay = message.type === 'text'
  ? randomBetween(700, 1500)
  : randomBetween(350, 800);
```

这样多气泡回复会更像真人边想边发。

---

## 主动消息流程

主动消息已经有 `typingIndicator`。

可以调整成：

- 如果用户在聊天页，AI 主动消息触发时先显示“麻衣 正在输入...”。
- 等 800 到 1500 毫秒后再开始发送第一条。
- 如果用户不在聊天页，不显示输入状态，只保留现有顶部弹窗和红点。

这样不会让后台主动消息显得突兀。

---

## 朋友圈状态

修改 `checkMomentProactive()`。

当 AI 开始检查朋友圈，并且用户正在朋友圈页：

```js
if (state.currentView === 'moments') {
  showMomentStatus(`${state.character.name}刚刚看了你的朋友圈`);
}
```

如果后端返回了点赞、评论或 AI 发动态：

- 调用 `refreshMoments()`。
- 隐藏 `momentStatus`。
- 如果用户不在朋友圈页，继续走现有红点和弹窗。

如果后端返回 `skipped: true`：

- 保持状态 2 秒左右自动隐藏。
- 不弹窗，不红点。

---

## 样式

在 `web/style.css` 增加：

```css
.read-status {
  padding: 4px 16px;
  background: #e9e9e9;
  color: #9ca3af;
  font-size: 12px;
}

.moment-status {
  padding: 7px 16px;
  background: #fff;
  color: #9ca3af;
  font-size: 12px;
}
```

如果觉得状态太显眼，可以把字体调到 `11px`，颜色改成 `#b8bdc5`。

---

## 测试清单

### 聊天

- 发送一条消息后，不会立刻调用 AI。
- 10 秒内继续发送多条消息，都会进入同一批缓存。
- 10 秒无新消息后，才显示“麻衣 已读”。
- 只有 `pendingUserMessages.length > 0` 时才调用 AI。
- 用户不发消息时，10 秒计时器不会空转调用 AI。
- 麻衣已读后，输入框和发送按钮会锁住。
- 麻衣回复完成后，输入框会解锁。
- 如果用户已读前在输入框里打了一半，回复完成后草稿还在。
- AI 回复前，会显示“麻衣 正在输入...”。
- AI 多条回复时，输入框不能继续发送。
- AI 回复完成后，已读和输入状态都消失。
- 请求失败时，状态不会卡住。

### 主动消息

- 在聊天页时，主动消息出现前会显示正在输入。
- 不在聊天页时，主动消息仍然只显示红点和顶部弹窗。

### 朋友圈

- 停留在朋友圈页时，AI 检查会短暂显示“麻衣刚刚看了你的朋友圈”。
- AI 点赞后，朋友圈点赞行正常刷新。
- AI 评论后，评论正常刷新。
- 不在朋友圈页时，点赞和评论仍会触发红点和弹窗。

---

## 今天完成范围

今天建议只做：

- 聊天未读缓存队列，10 秒后批量已读。
- 只有缓存有消息时才调用 AI，减少 token 和请求次数。
- 已读后锁输入框，回复完成后解锁并保留草稿。
- 聊天正在输入节奏优化。
- 朋友圈“刚刚看了你的朋友圈”状态。

暂时不做：

- 真正的消息已读回执入库。
- 多设备同步。
- 每条消息独立已读状态。
- 朋友圈访问记录历史。

这些都可以后面再扩展。
