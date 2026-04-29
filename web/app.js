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

  if (!messages.length) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = '还没有聊天记录，发第一条消息试试吧。';
    messageList.appendChild(empty);
    return;
  }

  messages.forEach((message) => {
    messageList.appendChild(createBubble(message));
  });
  scrollToBottom();
}

function appendMessage(message) {
  const empty = messageList.querySelector('.empty-state');
  if (empty) {
    empty.remove();
  }
  messageList.appendChild(createBubble(message));
  scrollToBottom();
}

function setLoading(loading) {
  sendBtn.disabled = loading;
  clearBtn.disabled = loading;
  messageInput.disabled = loading;
  typingIndicator.classList.toggle('hidden', !loading);
}

async function handleSend() {
  const text = messageInput.value.trim();
  if (!text) return;

  const optimistic = {
    sender: 'user',
    type: 'text',
    content: text,
  };
  appendMessage(optimistic);
  playMessageSound('send');
  messageInput.value = '';
  setLoading(true);

  try {
    const result = await sendMessage(text);
    result.messages.forEach((message) => appendMessage(message));
    if (result.messages.length > 0) {
      playMessageSound('receive');
    }
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
  } catch (error) {
    renderMessages([]);
    appendMessage({
      sender: 'character',
      type: 'text',
      content: `初始化失败：${error.message}`,
    });
  }
})();
