const characterId = 'luna';

const messageList = document.getElementById('messageList');
const messageInput = document.getElementById('messageInput');
const sendBtn = document.getElementById('sendBtn');
const clearBtn = document.getElementById('clearBtn');
const typingIndicator = document.getElementById('typingIndicator');
const chatTitle = document.querySelector('.chat-title');
const chatSubtitle = document.querySelector('.chat-subtitle');

const state = {
  character: {
    name: '角色',
    avatar: '',
    user_avatar: '',
  },
  audioContext: null,
  audioReady: null,
  isLoading: false,
  proactiveTimer: null,
  proactiveBusy: false,
  lastVisibleMessageTime: null,
};

const proactiveDelayRange = {
  min: 60 * 1000,
  max: 180 * 1000,
};

const firstProactiveDelayRange = {
  min: 8 * 1000,
  max: 15 * 1000,
};

function getAudioContext() {
  if (!window.AudioContext && !window.webkitAudioContext) {
    return null;
  }
  if (!state.audioContext) {
    const AudioContextClass = window.AudioContext || window.webkitAudioContext;
    state.audioContext = new AudioContextClass();
  }
  return state.audioContext;
}

function unlockAudio() {
  const audioContext = getAudioContext();
  if (!audioContext || audioContext.state !== 'suspended') {
    return Promise.resolve(audioContext);
  }

  state.audioReady = audioContext.resume().then(() => audioContext).catch(() => null);
  return state.audioReady;
}

async function playTone(frequency, startOffset, duration, volume = 0.045) {
  const audioContext = await unlockAudio();
  if (!audioContext || audioContext.state !== 'running') return;

  const oscillator = audioContext.createOscillator();
  const gain = audioContext.createGain();
  const start = audioContext.currentTime + startOffset;
  const end = start + duration;

  oscillator.type = 'sine';
  oscillator.frequency.setValueAtTime(frequency, start);
  gain.gain.setValueAtTime(0.0001, start);
  gain.gain.exponentialRampToValueAtTime(volume, start + 0.012);
  gain.gain.exponentialRampToValueAtTime(0.0001, end);

  oscillator.connect(gain);
  gain.connect(audioContext.destination);
  oscillator.start(start);
  oscillator.stop(end + 0.02);
}

function playMessageSound(kind) {
  if (kind === 'send') {
    playTone(740, 0, 0.08, 0.11);
    playTone(1040, 0.075, 0.1, 0.095);
    return;
  }

  playTone(520, 0, 0.08, 0.085);
  playTone(690, 0.085, 0.1, 0.078);
}

window.addEventListener('pointerdown', unlockAudio, { once: true });
window.addEventListener('keydown', unlockAudio, { once: true });

async function fetchCharacter() {
  const response = await fetch(`/api/character?character_id=${characterId}`);
  if (!response.ok) {
    throw new Error('加载角色失败');
  }
  return response.json();
}

async function fetchMessages() {
  const response = await fetch(`/api/messages?character_id=${characterId}`);
  if (!response.ok) {
    throw new Error('加载消息失败');
  }
  return response.json();
}

async function sendMessage(text) {
  const response = await fetch('/api/chat/send', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ character_id: characterId, message: text }),
  });

  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error(data.error || '发送失败');
  }

  return response.json();
}

async function requestProactiveMessage() {
  const response = await fetch('/api/chat/proactive', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ character_id: characterId }),
  });

  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error(data.error || '主动消息获取失败');
  }

  return response.json();
}

async function clearMessages() {
  const response = await fetch(`/api/messages/clear?character_id=${characterId}`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error('清空失败');
  }
}

function scrollToBottom() {
  messageList.scrollTop = messageList.scrollHeight;
}

function getAvatarUrl(sender) {
  if (sender === 'user') {
    return state.character.user_avatar || 'https://placehold.co/64x64?text=U';
  }
  return state.character.avatar || 'https://placehold.co/64x64?text=AI';
}

function formatMessageTime(value) {
  const date = parseMessageDate(value);
  if (!date) {
    return '';
  }
  return date.toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

function parseMessageDate(value) {
  const date = value ? new Date(value.replace(' ', 'T')) : new Date();
  return Number.isNaN(date.getTime()) ? null : date;
}

function shouldShowTimeSeparator(message) {
  const date = parseMessageDate(message.created_at);
  if (!date) return false;

  if (!state.lastVisibleMessageTime) {
    state.lastVisibleMessageTime = date;
    return true;
  }

  const diffMs = date.getTime() - state.lastVisibleMessageTime.getTime();
  if (diffMs > 3 * 60 * 1000) {
    state.lastVisibleMessageTime = date;
    return true;
  }
  return false;
}

function createTimeSeparator(message) {
  const separator = document.createElement('div');
  separator.className = 'time-separator';
  separator.textContent = formatMessageTime(message.created_at);
  return separator;
}

function createBubble(message) {
  const row = document.createElement('div');
  row.className = `message-row ${message.sender}`;

  const isMediaMessage = message.type === 'sticker' || message.type === 'image';

  const avatar = document.createElement('img');
  avatar.className = 'avatar';
  avatar.src = getAvatarUrl(message.sender);
  avatar.alt = message.sender === 'user' ? '我的头像' : `${state.character.name || '角色'}头像`;

  const bubble = document.createElement('div');
  bubble.className = isMediaMessage ? 'bubble media-bubble' : 'bubble';

  if (isMediaMessage) {
    const img = document.createElement('img');
    img.src = message.content;
    img.alt = message.type === 'sticker' ? 'sticker' : 'generated image';
    img.className = message.type === 'sticker' ? 'sticker' : 'generated-image';
    bubble.appendChild(img);
  } else {
    bubble.textContent = message.content;
  }

  if (message.sender === 'character') {
    row.appendChild(avatar);
    row.appendChild(bubble);
  } else {
    row.appendChild(bubble);
    row.appendChild(avatar);
  }

  return row;
}

function renderMessages(messages) {
  messageList.innerHTML = '';
  state.lastVisibleMessageTime = null;

  if (!messages.length) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = '还没有聊天记录，发第一条消息试试吧。';
    messageList.appendChild(empty);
    return;
  }

  messages.forEach((message) => {
    if (shouldShowTimeSeparator(message)) {
      messageList.appendChild(createTimeSeparator(message));
    }
    messageList.appendChild(createBubble(message));
  });
  scrollToBottom();
}

function appendMessage(message) {
  const empty = messageList.querySelector('.empty-state');
  if (empty) {
    empty.remove();
  }
  if (shouldShowTimeSeparator(message)) {
    messageList.appendChild(createTimeSeparator(message));
  }
  messageList.appendChild(createBubble(message));
  scrollToBottom();
}

function wait(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function messageDelay(message) {
  if (!message || message.type !== 'text') {
    return 520;
  }
  const length = [...message.content].length;
  return Math.min(1200, Math.max(520, length * 34));
}

async function appendMessagesSequentially(messages, { playReceiveSound = false } = {}) {
  const queue = Array.isArray(messages) ? messages : [];
  for (let index = 0; index < queue.length; index += 1) {
    if (index > 0) {
      typingIndicator.classList.remove('hidden');
      await wait(messageDelay(queue[index - 1]));
    }
    typingIndicator.classList.add('hidden');
    appendMessage(queue[index]);
    if (playReceiveSound && index === 0) {
      playMessageSound('receive');
    }
  }
}

function setLoading(loading) {
  state.isLoading = loading;
  sendBtn.disabled = loading;
  clearBtn.disabled = loading;
  messageInput.disabled = loading;
  typingIndicator.classList.toggle('hidden', !loading);
}

function randomDelay({ min, max }) {
  return Math.floor(min + Math.random() * (max - min));
}

function scheduleProactiveMessage(delayMs) {
  window.clearTimeout(state.proactiveTimer);
  const delay = Number.isFinite(delayMs) ? delayMs : randomDelay(proactiveDelayRange);
  console.info(`主动消息检查已安排，约 ${Math.round(delay / 1000)} 秒后执行`);
  state.proactiveTimer = window.setTimeout(checkProactiveMessage, delay);
}

async function checkProactiveMessage() {
  const typedText = messageInput.value.trim();
  if (document.hidden || state.isLoading || state.proactiveBusy || typedText) {
    console.info('主动消息检查跳过', {
      hidden: document.hidden,
      loading: state.isLoading,
      busy: state.proactiveBusy,
      typing: Boolean(typedText),
    });
    scheduleProactiveMessage();
    return;
  }

  console.info('主动消息检查开始');
  state.proactiveBusy = true;
  typingIndicator.classList.remove('hidden');

  try {
    const result = await requestProactiveMessage();
    const messages = Array.isArray(result.messages) ? result.messages : [];
    if (!result.skipped && messages.length > 0) {
      console.info(`主动消息触发成功，共 ${messages.length} 条`);
      setLoading(true);
      await appendMessagesSequentially(messages, { playReceiveSound: true });
    } else if (result.next_check_after_seconds) {
      const jitter = randomDelay({ min: 8 * 1000, max: 25 * 1000 });
      console.info(`主动消息暂不触发，约 ${result.next_check_after_seconds} 秒后重试`);
      scheduleProactiveMessage((result.next_check_after_seconds * 1000) + jitter);
      return;
    } else {
      console.info('主动消息暂不触发，后端未要求具体重试时间', result);
    }
  } catch (error) {
    console.warn('主动消息检查失败', error);
  } finally {
    state.proactiveBusy = false;
    if (state.isLoading) {
      setLoading(false);
    } else {
      typingIndicator.classList.add('hidden');
    }
    scheduleProactiveMessage();
  }
}

async function handleSend() {
  if (state.isLoading) return;

  const text = messageInput.value.trim();
  if (!text) return;

  const optimistic = {
    sender: 'user',
    type: 'text',
    content: text,
    created_at: new Date().toISOString(),
  };
  appendMessage(optimistic);
  playMessageSound('send');
  messageInput.value = '';
  setLoading(true);

  try {
    const result = await sendMessage(text);
    const messages = Array.isArray(result.messages) ? result.messages : [];
    await appendMessagesSequentially(messages, { playReceiveSound: messages.length > 0 });
    scheduleProactiveMessage();
  } catch (error) {
    appendMessage({
      sender: 'character',
      type: 'text',
      content: `出错了：${error.message}`,
    });
  } finally {
    setLoading(false);
    messageInput.focus();
  }
}

sendBtn.addEventListener('click', handleSend);
messageInput.addEventListener('keydown', (event) => {
  if (event.key === 'Enter') {
    handleSend();
  }
});

clearBtn.addEventListener('click', async () => {
  if (!confirm(`要清空和${state.character.name || '角色'}的聊天记录吗？`)) {
    return;
  }

  try {
    setLoading(true);
    await clearMessages();
    renderMessages([]);
    scheduleProactiveMessage();
  } catch (error) {
    appendMessage({
      sender: 'character',
      type: 'text',
      content: `清空失败：${error.message}`,
    });
  } finally {
    setLoading(false);
  }
});

function applyCharacterUI(character) {
  state.character = character;
  document.title = `${character.name} - DimensionMessenger Demo`;
  chatTitle.textContent = character.name;
  chatSubtitle.textContent = character.relationship || '沉浸式二次元聊天 Demo';
  typingIndicator.textContent = `${character.name} 正在输入...`;
  messageInput.placeholder = `输入消息，和${character.name}聊聊天...`;
}

(async function init() {
  try {
    const [character, messages] = await Promise.all([fetchCharacter(), fetchMessages()]);
    applyCharacterUI(character);
    renderMessages(messages);
    scheduleProactiveMessage(randomDelay(firstProactiveDelayRange));
  } catch (error) {
    renderMessages([]);
    appendMessage({
      sender: 'character',
      type: 'text',
      content: `初始化失败：${error.message}`,
    });
  }
})();
