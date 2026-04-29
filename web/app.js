const characterId = 'luna';

const messageList = document.getElementById('messageList');
const messageInput = document.getElementById('messageInput');
const sendBtn = document.getElementById('sendBtn');
const clearBtn = document.getElementById('clearBtn');
const typingIndicator = document.getElementById('typingIndicator');
const chatTitle = document.querySelector('.chat-title');
const chatSubtitle = document.querySelector('.chat-subtitle');
const backBtn = document.getElementById('backBtn');
const chatView = document.getElementById('chatView');
const appViews = document.querySelectorAll('.app-view');
const bottomTabs = document.querySelector('.bottom-tabs');
const tabButtons = document.querySelectorAll('.bottom-tab');
const maiConversation = document.getElementById('maiConversation');
const maiContact = document.getElementById('maiContact');
const conversationPreview = document.getElementById('conversationPreview');
const conversationTime = document.getElementById('conversationTime');
const momentInput = document.getElementById('momentInput');
const postMomentBtn = document.getElementById('postMomentBtn');
const momentsList = document.getElementById('momentsList');
const notificationToast = document.getElementById('notificationToast');
const notificationAvatar = document.getElementById('notificationAvatar');
const notificationTitle = document.getElementById('notificationTitle');
const notificationBody = document.getElementById('notificationBody');

const state = {
  character: {
    name: '角色',
    avatar: '',
    user_avatar: '',
  },
  messages: [],
  currentView: 'messages',
  audioContext: null,
  audioReady: null,
  isLoading: false,
  proactiveTimer: null,
  momentTimer: null,
  proactiveBusy: false,
  momentBusy: false,
  lastVisibleMessageTime: null,
  moments: [],
  unreadChat: false,
  unreadMoments: false,
  toastTimer: null,
  notificationQueue: [],
  toastActive: false,
};

const proactiveDelayRange = {
  min: 60 * 1000,
  max: 180 * 1000,
};

const firstProactiveDelayRange = {
  min: 8 * 1000,
  max: 15 * 1000,
};

const momentCheckDelayRange = {
  min: 90 * 1000,
  max: 240 * 1000,
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

async function fetchMoments() {
  const response = await fetch(`/api/moments?character_id=${characterId}`);
  if (!response.ok) {
    throw new Error('加载朋友圈失败');
  }
  return response.json();
}

async function createMoment(content) {
  const response = await fetch('/api/moments', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ character_id: characterId, content }),
  });

  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error(data.error || '发表失败');
  }
  return response.json();
}

async function requestMomentProactive() {
  const response = await fetch('/api/moments/proactive', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ character_id: characterId }),
  });

  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error(data.error || '朋友圈检查失败');
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

function setHeader(title, subtitle = '') {
  chatTitle.textContent = title;
  chatSubtitle.textContent = subtitle;
}

function updateUnreadBadges() {
  const messagesTab = document.querySelector('.bottom-tab[data-view="messages"]');
  const momentsTab = document.querySelector('.bottom-tab[data-view="moments"]');

  messagesTab?.classList.toggle('has-unread', state.unreadChat && state.currentView !== 'messages' && state.currentView !== 'chat');
  momentsTab?.classList.toggle('has-unread', state.unreadMoments && state.currentView !== 'moments');
  maiConversation.classList.toggle('has-unread', state.unreadChat);
}

function setUnread(kind, value) {
  if (kind === 'chat') {
    state.unreadChat = value;
  }
  if (kind === 'moments') {
    state.unreadMoments = value;
  }
  updateUnreadBadges();
}

function getMessagePreview(message) {
  if (!message) return '点开看看';
  if (message.type === 'sticker') return '[表情包]';
  if (message.type === 'image') return '[图片]';
  return message.content || '点开看看';
}

function showNextNotification() {
  if (state.toastActive || state.notificationQueue.length === 0) return;

  window.clearTimeout(state.toastTimer);
  const { title, body, avatar, onClick } = state.notificationQueue.shift();
  state.toastActive = true;
  notificationTitle.textContent = title;
  notificationBody.textContent = body;
  notificationAvatar.src = avatar || state.character.avatar || 'https://placehold.co/64x64?text=AI';
  notificationToast.onclick = () => {
    window.clearTimeout(state.toastTimer);
    notificationToast.classList.remove('show');
    state.toastActive = false;
    onClick?.();
    window.setTimeout(showNextNotification, 180);
  };
  notificationToast.classList.add('show');

  state.toastTimer = window.setTimeout(() => {
    notificationToast.classList.remove('show');
    state.toastActive = false;
    window.setTimeout(showNextNotification, 180);
  }, 4200);
}

function showNotification(notification) {
  state.notificationQueue.push(notification);
  showNextNotification();
}

function setMainView(viewName) {
  state.currentView = viewName;
  chatView.classList.add('hidden');
  bottomTabs.classList.remove('hidden');
  backBtn.classList.add('hidden');
  clearBtn.classList.add('hidden');

  appViews.forEach((view) => {
    view.classList.toggle('active-view', view.id === `${viewName}View`);
  });
  tabButtons.forEach((button) => {
    button.classList.toggle('active', button.dataset.view === viewName);
  });

  const titles = {
    messages: ['消息', 'DimensionMessenger'],
    contacts: ['联系人', '只有真正重要的人'],
    moments: ['朋友圈', '动态'],
    profile: ['我的', '本地资料'],
  };
  if (viewName === 'moments') {
    setUnread('moments', false);
  }
  setHeader(...titles[viewName]);
  updateUnreadBadges();
}

function openChat() {
  state.currentView = 'chat';
  setUnread('chat', false);
  appViews.forEach((view) => view.classList.remove('active-view'));
  chatView.classList.remove('hidden');
  bottomTabs.classList.add('hidden');
  backBtn.classList.remove('hidden');
  clearBtn.classList.remove('hidden');
  setHeader(state.character.name, state.character.relationship || '沉浸式二次元聊天 Demo');
  renderMessages(state.messages);
  messageInput.focus();
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

function formatListTime(value) {
  const date = parseMessageDate(value);
  if (!date) return '';

  const now = new Date();
  const sameDay = date.toDateString() === now.toDateString();
  if (sameDay) {
    return formatMessageTime(value);
  }
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

function formatMomentTime(value) {
  const date = parseMessageDate(value);
  if (!date) return '';

  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  if (diffMs < 60 * 1000) return '刚刚';
  if (diffMs < 60 * 60 * 1000) return `${Math.floor(diffMs / 60000)}分钟前`;
  if (date.toDateString() === now.toDateString()) return `今天 ${formatMessageTime(value)}`;
  return `${date.getMonth() + 1}月${date.getDate()}日 ${formatMessageTime(value)}`;
}

function parseMessageDate(value) {
  const date = value ? new Date(value.replace(' ', 'T')) : new Date();
  return Number.isNaN(date.getTime()) ? null : date;
}

function getMomentAuthorName(author) {
  return author === 'character' ? state.character.name : '我';
}

function getMomentAvatar(author) {
  if (author === 'character') {
    return state.character.avatar || 'https://placehold.co/64x64?text=AI';
  }
  return state.character.user_avatar || 'https://placehold.co/64x64?text=U';
}

function createMomentCard(moment) {
  const card = document.createElement('article');
  card.className = 'moment-card';

  const avatar = document.createElement('img');
  avatar.className = 'moment-avatar';
  avatar.src = getMomentAvatar(moment.author);
  avatar.alt = `${getMomentAuthorName(moment.author)}头像`;

  const body = document.createElement('div');
  body.className = 'moment-body';

  const name = document.createElement('div');
  name.className = 'moment-name';
  name.textContent = getMomentAuthorName(moment.author);

  const content = document.createElement('p');
  content.textContent = moment.content;

  body.appendChild(name);
  body.appendChild(content);

  if (moment.image_url) {
    const image = document.createElement('img');
    image.className = 'moment-image';
    image.src = moment.image_url;
    image.alt = '朋友圈图片';
    body.appendChild(image);
  }

  const actions = document.createElement('div');
  actions.className = 'moment-actions';
  const time = document.createElement('span');
  time.textContent = formatMomentTime(moment.created_at);
  const menu = document.createElement('button');
  menu.type = 'button';
  menu.textContent = '··';
  actions.appendChild(time);
  actions.appendChild(menu);
  body.appendChild(actions);

  const hasLikes = Array.isArray(moment.likes) && moment.likes.length > 0;
  const hasComments = Array.isArray(moment.comments) && moment.comments.length > 0;
  if (hasLikes || hasComments) {
    const comments = document.createElement('div');
    comments.className = 'moment-comments';
    if (hasLikes) {
      const likes = document.createElement('div');
      likes.className = 'moment-likes';
      const heart = document.createElement('span');
      heart.textContent = '♥';
      likes.appendChild(heart);
      likes.append(` ${moment.likes.map((like) => getMomentAuthorName(like.author)).join('、')}`);
      comments.appendChild(likes);
    }
    (moment.comments || []).forEach((comment) => {
      const item = document.createElement('div');
      const author = document.createElement('strong');
      author.textContent = getMomentAuthorName(comment.author);
      item.appendChild(author);
      item.append(`：${comment.content}`);
      comments.appendChild(item);
    });
    body.appendChild(comments);
  }

  card.appendChild(avatar);
  card.appendChild(body);
  return card;
}

function renderMoments(moments) {
  state.moments = Array.isArray(moments) ? moments : [];
  momentsList.innerHTML = '';
  if (state.moments.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = '还没有朋友圈，发第一条试试吧。';
    momentsList.appendChild(empty);
    return;
  }
  state.moments.forEach((moment) => {
    momentsList.appendChild(createMomentCard(moment));
  });
}

async function refreshMoments() {
  const moments = await fetchMoments();
  renderMoments(moments);
}

function updateConversationSummary(messages = state.messages) {
  const latest = [...messages].reverse().find((message) => message.sender === 'character' || message.sender === 'user');
  if (!latest) {
    conversationPreview.textContent = '点开和她聊天';
    conversationTime.textContent = '';
    return;
  }

  if (latest.type === 'sticker') {
    conversationPreview.textContent = '[表情包]';
  } else if (latest.type === 'image') {
    conversationPreview.textContent = '[图片]';
  } else {
    conversationPreview.textContent = latest.content;
  }
  conversationTime.textContent = formatListTime(latest.created_at);
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
  state.messages = Array.isArray(messages) ? messages : [];
  state.lastVisibleMessageTime = null;

  if (!state.messages.length) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = '还没有聊天记录，发第一条消息试试吧。';
    messageList.appendChild(empty);
    updateConversationSummary();
    return;
  }

  state.messages.forEach((message) => {
    if (shouldShowTimeSeparator(message)) {
      messageList.appendChild(createTimeSeparator(message));
    }
    messageList.appendChild(createBubble(message));
  });
  updateConversationSummary();
  scrollToBottom();
}

function appendMessage(message) {
  const empty = messageList.querySelector('.empty-state');
  if (empty) {
    empty.remove();
  }
  state.messages.push(message);
  updateConversationSummary();
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

function scheduleMomentCheck(delayMs) {
  window.clearTimeout(state.momentTimer);
  const delay = Number.isFinite(delayMs) ? delayMs : randomDelay(momentCheckDelayRange);
  console.info(`朋友圈 AI 检查已安排，约 ${Math.round(delay / 1000)} 秒后执行`);
  state.momentTimer = window.setTimeout(checkMomentProactive, delay);
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
      if (state.currentView !== 'chat') {
        state.messages.push(...messages);
        updateConversationSummary();
        setUnread('chat', true);
        showNotification({
          title: state.character.name,
          body: getMessagePreview(messages[messages.length - 1]),
          avatar: state.character.avatar,
          onClick: openChat,
        });
        playMessageSound('receive');
        return;
      }
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

async function checkMomentProactive() {
  if (document.hidden || state.momentBusy) {
    console.info('朋友圈 AI 检查跳过', {
      hidden: document.hidden,
      busy: state.momentBusy,
    });
    scheduleMomentCheck();
    return;
  }

  console.info('朋友圈 AI 检查开始');
  state.momentBusy = true;
  try {
    const result = await requestMomentProactive();
    if (!result.skipped) {
      const shouldNotify = state.currentView !== 'moments' && (result.like || result.comment || result.moment);
      if (result.like) {
        console.info('朋友圈 AI 已点赞', result.like);
      }
      if (result.comment) {
        console.info('朋友圈 AI 已评论', result.comment);
      }
      if (result.moment) {
        console.info('朋友圈 AI 已发动态', result.moment);
      }
      await refreshMoments();
      if (shouldNotify) {
        setUnread('moments', true);
        const openMoments = () => {
          setMainView('moments');
          refreshMoments().catch((error) => console.warn(error));
        };
        if (result.like) {
          showNotification({
            title: '朋友圈有新点赞',
            body: `${state.character.name}赞了你的朋友圈`,
            avatar: state.character.avatar,
            onClick: openMoments,
          });
        }
        if (result.comment) {
          showNotification({
            title: '朋友圈有新评论',
            body: result.comment.content || '点开看看',
            avatar: state.character.avatar,
            onClick: openMoments,
          });
        }
        if (result.moment) {
          showNotification({
            title: `${state.character.name}发了朋友圈`,
            body: result.moment.content || '点开看看',
            avatar: state.character.avatar,
            onClick: openMoments,
          });
        }
      }
    } else {
      console.info('朋友圈 AI 暂不评论也不发动态', result);
    }
  } catch (error) {
    console.warn('朋友圈 AI 检查失败', error);
  } finally {
    state.momentBusy = false;
    scheduleMomentCheck();
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

backBtn.addEventListener('click', () => {
  setMainView('messages');
});

maiConversation.addEventListener('click', openChat);
maiContact.addEventListener('click', openChat);

tabButtons.forEach((button) => {
  button.addEventListener('click', () => {
    setMainView(button.dataset.view);
    if (button.dataset.view === 'moments') {
      refreshMoments().catch((error) => console.warn(error));
    }
  });
});

postMomentBtn.addEventListener('click', async () => {
  const content = momentInput.value.trim();
  if (!content) return;

  postMomentBtn.disabled = true;
  try {
    await createMoment(content);
    momentInput.value = '';
    await refreshMoments();
    scheduleMomentCheck(20 * 1000);
  } catch (error) {
    alert(error.message);
  } finally {
    postMomentBtn.disabled = false;
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
  typingIndicator.textContent = `${character.name} 正在输入...`;
  messageInput.placeholder = `输入消息，和${character.name}聊聊天...`;

  document.querySelectorAll('.conversation-avatar, .contact-avatar, .moment-card .moment-avatar').forEach((avatar) => {
    avatar.src = character.avatar || 'https://placehold.co/64x64?text=AI';
  });
  const profileAvatar = document.querySelector('.profile-avatar');
  profileAvatar.src = character.user_avatar || 'https://placehold.co/72x72?text=U';
  const momentsSelfAvatar = document.querySelector('.moments-self-avatar');
  momentsSelfAvatar.src = character.user_avatar || 'https://placehold.co/72x72?text=U';
  document.querySelector('.conversation-name').textContent = character.name;
  document.querySelector('.contact-name').textContent = character.name;
  document.querySelector('.contact-note').textContent = character.relationship || '联系人';
}

(async function init() {
  try {
    const [character, messages, moments] = await Promise.all([fetchCharacter(), fetchMessages(), fetchMoments()]);
    applyCharacterUI(character);
    state.messages = messages;
    renderMoments(moments);
    updateConversationSummary();
    setMainView('messages');
    scheduleProactiveMessage(randomDelay(firstProactiveDelayRange));
    scheduleMomentCheck(randomDelay(momentCheckDelayRange));
  } catch (error) {
    setMainView('messages');
    conversationPreview.textContent = `初始化失败：${error.message}`;
  }
})();
