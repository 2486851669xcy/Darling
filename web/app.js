const characterId = 'luna';

const messageList = document.getElementById('messageList');
const messageInput = document.getElementById('messageInput');
const sendBtn = document.getElementById('sendBtn');
const clearBtn = document.getElementById('clearBtn');
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
const momentStatus = document.getElementById('momentStatus');
const momentsList = document.getElementById('momentsList');
const notificationToast = document.getElementById('notificationToast');
const notificationAvatar = document.getElementById('notificationAvatar');
const notificationTitle = document.getElementById('notificationTitle');
const notificationBody = document.getElementById('notificationBody');
const wechatLoginBtn = document.getElementById('wechatLoginBtn');
const wechatLoginIcon = document.getElementById('wechatLoginIcon');
const wechatUserAvatar = document.getElementById('wechatUserAvatar');
const wechatLoginTitle = document.getElementById('wechatLoginTitle');
const wechatLoginDetail = document.getElementById('wechatLoginDetail');
const wechatLoginStatus = document.getElementById('wechatLoginStatus');
const wechatLoginFeedback = document.getElementById('wechatLoginFeedback');
const wechatAccountActions = document.getElementById('wechatAccountActions');
const wechatLogoutBtn = document.getElementById('wechatLogoutBtn');
const wechatLogoutLabel = document.getElementById('wechatLogoutLabel');

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
  momentStatusTimer: null,
  pendingUserMessages: [],
  pendingSendTimer: null,
  isWaitingForAI: false,
  unreadBatchDelay: 10 * 1000,
  lastUserBubble: null,
  pendingUserRows: [],
  typingBubble: null,
  session: null,
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
  min: 45 * 1000,
  max: 120 * 1000,
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

function normalizeWeChatSession(data) {
  const rawUser = data && data.user && typeof data.user === 'object' ? data.user : {};
  return {
    authenticated: data && data.authenticated === true,
    wechatEnabled: data && data.wechat_enabled === true,
    user: {
      nickname: typeof rawUser.nickname === 'string' ? rawUser.nickname.trim() : '',
      avatarUrl: typeof rawUser.avatar_url === 'string' ? rawUser.avatar_url.trim() : '',
    },
  };
}

function setWeChatLoginFeedback(message, kind = '') {
  wechatLoginFeedback.textContent = message;
  wechatLoginFeedback.classList.toggle('is-success', kind === 'success');
  wechatLoginFeedback.classList.toggle('is-error', kind === 'error');
}

function setWeChatLogoutBusy(isBusy) {
  wechatLogoutBtn.disabled = isBusy;
  wechatLogoutBtn.setAttribute('aria-busy', String(isBusy));
  wechatLogoutLabel.textContent = isBusy ? '正在退出…' : '退出微信登录';
}

function renderWeChatLoginState(session) {
  wechatLoginBtn.classList.remove('is-checking', 'is-ready', 'is-unavailable', 'is-authenticated');
  wechatAccountActions.hidden = !session.authenticated;
  setWeChatLogoutBusy(false);

  if (session.authenticated) {
    const nickname = session.user.nickname || '微信用户';
    const hasAvatar = Boolean(session.user.avatarUrl);

    wechatLoginBtn.classList.add('is-authenticated');
    wechatLoginBtn.disabled = true;
    wechatLoginBtn.setAttribute('aria-label', '已登录微信账号' + (session.user.nickname ? '：' + session.user.nickname : ''));
    wechatLoginTitle.textContent = nickname;
    wechatLoginDetail.textContent = '已通过微信登录，当前数据已绑定到此账号。';
    wechatLoginStatus.textContent = '已登录';
    wechatLoginIcon.hidden = hasAvatar;
    wechatUserAvatar.hidden = !hasAvatar;

    if (hasAvatar) {
      wechatUserAvatar.src = session.user.avatarUrl;
      wechatUserAvatar.alt = nickname + '的微信头像';
    } else {
      wechatUserAvatar.removeAttribute('src');
      wechatUserAvatar.alt = '';
    }
    return;
  }

  wechatUserAvatar.hidden = true;
  wechatUserAvatar.removeAttribute('src');
  wechatUserAvatar.alt = '';
  wechatLoginIcon.hidden = false;
  wechatLoginTitle.textContent = '微信扫码登录';

  if (!session.wechatEnabled) {
    wechatLoginBtn.classList.add('is-unavailable');
    wechatLoginBtn.disabled = true;
    wechatLoginBtn.setAttribute('aria-label', '微信扫码登录尚未启用');
    wechatLoginDetail.textContent = '管理员尚未启用微信扫码登录；当前继续使用本机匿名身份。';
    wechatLoginStatus.textContent = '未启用';
    return;
  }

  wechatLoginBtn.classList.add('is-ready');
  wechatLoginBtn.disabled = false;
  wechatLoginBtn.setAttribute('aria-label', '使用微信扫码登录');
  wechatLoginDetail.textContent = '点击后前往微信开放平台扫码，成功后返回当前页面。';
  wechatLoginStatus.textContent = '去登录';
}

function consumeWeChatLoginCallback() {
  const url = new URL(window.location.href);
  const result = url.searchParams.get('wechat_login');
  if (result === null) {
    return null;
  }

  url.searchParams.delete('wechat_login');
  window.history.replaceState(window.history.state, '', [url.pathname, url.search, url.hash].join(''));
  return result === 'success' || result === 'error' ? result : null;
}

function applyWeChatLoginCallback(result, session) {
  if (result === 'error') {
    setWeChatLoginFeedback('微信登录未完成，请重新扫码尝试。', 'error');
    return;
  }
  if (result === 'success' && session.authenticated) {
    setWeChatLoginFeedback('微信登录成功，登录状态已同步。', 'success');
    return;
  }
  if (result === 'success') {
    setWeChatLoginFeedback('微信已返回，但登录状态尚未生效，请重新扫码尝试。', 'error');
  }
}

async function ensureSession() {
  const response = await fetch('/api/session', {
    credentials: 'same-origin',
    cache: 'no-store',
  });
  if (response.status === 503) {
    const statusResponse = await fetch('/api/auth/wechat/status', {
      credentials: 'same-origin',
      cache: 'no-store',
    }).catch(() => null);
    const status = statusResponse && statusResponse.ok ? await statusResponse.json().catch(() => null) : null;
    if (status && status.wechat_enabled === true) {
      const session = normalizeWeChatSession({
        authenticated: false,
        wechat_enabled: true,
      });
      state.session = session;
      renderWeChatLoginState(session);
      setWeChatLoginFeedback('当前匿名会话容量已满，聊天和朋友圈暂不可用；你仍可使用微信扫码登录。', 'error');
      return session;
    }
  }
  if (!response.ok) {
    throw new Error('初始化用户身份失败');
  }
  const data = await response.json().catch(() => null);
  if (!data || data.ok !== true) {
    throw new Error('用户身份响应无效');
  }
  const session = normalizeWeChatSession(data);
  state.session = session;
  renderWeChatLoginState(session);
  return session;
}

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

async function sendMessageBatch(messages) {
  const response = await fetch('/api/chat/send_batch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ character_id: characterId, messages }),
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
    const status = document.createElement('span');
    status.className = 'bubble-read-status';
    status.setAttribute('aria-hidden', 'true');
    status.innerHTML = '<span class="read-check" aria-hidden="true">✓</span><span class="read-label">已读</span>';
    row.appendChild(status);
    row.appendChild(bubble);
    row.appendChild(avatar);
  }

  return row;
}

function createTypingBubble() {
  const row = document.createElement('div');
  row.className = 'message-row character typing-row';

  const avatar = document.createElement('img');
  avatar.className = 'avatar';
  avatar.src = getAvatarUrl('character');
  avatar.alt = `${state.character.name || '角色'}头像`;

  const bubble = document.createElement('div');
  bubble.className = 'bubble typing-bubble';
  bubble.innerHTML = '<span></span><span></span><span></span>';

  row.appendChild(avatar);
  row.appendChild(bubble);
  return row;
}

function showTypingBubble() {
  if (state.typingBubble) return;
  const empty = messageList.querySelector('.empty-state');
  if (empty) {
    empty.remove();
  }
  state.typingBubble = createTypingBubble();
  messageList.appendChild(state.typingBubble);
  scrollToBottom();
}

function hideTypingBubble() {
  state.typingBubble?.remove();
  state.typingBubble = null;
}

function hasLaterCharacterMessage(messages, index) {
  return messages.slice(index + 1).some((message) => message.sender === 'character');
}

function setMessageRowRead(row, isRead) {
  const status = row?.querySelector('.bubble-read-status');
  if (!status) return;

  status.classList.toggle('read', isRead);
  status.setAttribute('aria-hidden', String(!isRead));
}

function markUserMessageRead(message, row) {
  if (message?.sender !== 'user') return;
  message.read = true;
  setMessageRowRead(row, true);
}

function markVisibleUserMessagesRead() {
  state.messages.forEach((message) => {
    if (message.sender === 'user') {
      message.read = true;
    }
  });
  messageList.querySelectorAll('.message-row.user').forEach((row) => {
    setMessageRowRead(row, true);
  });
}

function renderMessages(messages) {
  messageList.innerHTML = '';
  state.messages = Array.isArray(messages) ? messages : [];
  state.lastVisibleMessageTime = null;
  state.lastUserBubble = null;
  state.pendingUserRows = [];

  if (!state.messages.length) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = '还没有聊天记录，发第一条消息试试吧。';
    messageList.appendChild(empty);
    updateConversationSummary();
    return;
  }

  state.messages.forEach((message, index) => {
    if (shouldShowTimeSeparator(message)) {
      messageList.appendChild(createTimeSeparator(message));
    }
    const row = createBubble(message);
    if (message.sender === 'user' && (message.read === true || hasLaterCharacterMessage(state.messages, index))) {
      markUserMessageRead(message, row);
    }
    messageList.appendChild(row);
    if (message.sender === 'user') {
      state.lastUserBubble = row;
    }
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
  if (message.sender === 'character') {
    markVisibleUserMessagesRead();
  }
  updateConversationSummary();
  if (shouldShowTimeSeparator(message)) {
    messageList.appendChild(createTimeSeparator(message));
  }
  const row = createBubble(message);
  messageList.appendChild(row);
  if (message.sender === 'user') {
    state.lastUserBubble = row;
    state.pendingUserRows.push(row);
  }
  scrollToBottom();
}

function wait(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function randomBetween(min, max) {
  return Math.floor(min + Math.random() * (max - min + 1));
}

function markPendingUserMessagesRead(batch) {
  batch.forEach((item) => {
    markUserMessageRead(item.message, item.row);
  });
  state.pendingUserRows = [];
}

function clearInlineReadStatus() {
  document.querySelectorAll('.bubble-read-status').forEach((status) => {
    status.classList.remove('read');
    status.setAttribute('aria-hidden', 'true');
  });
}

function showMomentStatus(text, duration = 2200) {
  window.clearTimeout(state.momentStatusTimer);
  momentStatus.textContent = text;
  momentStatus.classList.remove('hidden');
  state.momentStatusTimer = window.setTimeout(hideMomentStatus, duration);
}

function hideMomentStatus() {
  window.clearTimeout(state.momentStatusTimer);
  momentStatus.classList.add('hidden');
}

function messageDelay(message) {
  if (!message || message.type !== 'text') {
    return randomBetween(350, 800);
  }
  const length = [...message.content].length;
  return Math.min(1500, Math.max(700, length * 34));
}

async function appendMessagesSequentially(messages, { playReceiveSound = false } = {}) {
  const queue = Array.isArray(messages) ? messages : [];
  for (let index = 0; index < queue.length; index += 1) {
    showTypingBubble();
    await wait(index === 0 ? randomBetween(600, 1200) : messageDelay(queue[index - 1]));
    hideTypingBubble();
    appendMessage(queue[index]);
    if (playReceiveSound && index === 0) {
      playMessageSound('receive');
    }
  }
}

function setLoading(loading, { showTyping = loading } = {}) {
  state.isLoading = loading;
  sendBtn.disabled = loading;
  clearBtn.disabled = loading;
  messageInput.disabled = loading;
  if (showTyping) {
    showTypingBubble();
  } else {
    hideTypingBubble();
  }
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

function scheduleFlushUserMessages() {
  window.clearTimeout(state.pendingSendTimer);
  if (state.pendingUserMessages.length === 0 || state.isWaitingForAI) {
    return;
  }

  console.info(`用户消息已缓存 ${state.pendingUserMessages.length} 条，约 ${Math.round(state.unreadBatchDelay / 1000)} 秒后已读`);
  state.pendingSendTimer = window.setTimeout(flushUserMessages, state.unreadBatchDelay);
}

async function flushUserMessages() {
  window.clearTimeout(state.pendingSendTimer);
  if (state.isWaitingForAI || state.pendingUserMessages.length === 0) {
    return;
  }

  const batch = [...state.pendingUserMessages];
  state.pendingUserMessages = [];
  state.isWaitingForAI = true;
  const draftText = messageInput.value;
  const texts = batch.map((item) => item.text);

  markPendingUserMessagesRead(batch);
  setLoading(true, { showTyping: false });

  try {
    await wait(randomBetween(700, 1300));
    showTypingBubble();
    const result = await sendMessageBatch(texts);
    if (Array.isArray(result.user_messages)) {
      result.user_messages.forEach((savedMessage, index) => {
        if (batch[index]?.message) {
          Object.assign(batch[index].message, savedMessage);
        }
      });
    }
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
    state.isWaitingForAI = false;
    setLoading(false);
    hideTypingBubble();
    if (messageInput.value === '') {
      messageInput.value = draftText;
    }
    messageInput.focus();
    if (state.pendingUserMessages.length > 0) {
      scheduleFlushUserMessages();
    }
  }
}

async function checkProactiveMessage() {
  const typedText = messageInput.value.trim();
  if (document.hidden || state.isLoading || state.proactiveBusy || typedText || state.pendingUserMessages.length > 0) {
    console.info('主动消息检查跳过', {
      hidden: document.hidden,
      loading: state.isLoading,
      busy: state.proactiveBusy,
      typing: Boolean(typedText),
      pending: state.pendingUserMessages.length,
    });
    scheduleProactiveMessage();
    return;
  }

  console.info('主动消息检查开始');
  state.proactiveBusy = true;
  showTypingBubble();

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
      hideTypingBubble();
    }
    scheduleProactiveMessage();
  }
}

async function checkMomentProactive() {
  if (state.momentBusy) {
    console.info('朋友圈 AI 检查跳过', {
      busy: state.momentBusy,
    });
    scheduleMomentCheck();
    return;
  }

  console.info('朋友圈 AI 检查开始');
  state.momentBusy = true;
  if (state.currentView === 'moments') {
    showMomentStatus(`${state.character.name}刚刚看了你的朋友圈`);
  }
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
      hideMomentStatus();
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
      if (result.reason === 'no_new_moment_signal') {
        console.info('朋友圈暂无新动态信号，本次不调用 AI', result);
      } else {
        console.info('朋友圈 AI 暂不评论也不发动态', result);
      }
    }
  } catch (error) {
    console.warn('朋友圈 AI 检查失败', error);
    hideMomentStatus();
  } finally {
    state.momentBusy = false;
    scheduleMomentCheck();
  }
}

function handleSend() {
  if (state.isLoading || state.isWaitingForAI) return;

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
  state.pendingUserMessages.push({
    text,
    row: state.lastUserBubble,
    message: optimistic,
  });
  messageInput.value = '';
  scheduleFlushUserMessages();
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

wechatLoginBtn.addEventListener('click', () => {
  if (!state.session || !state.session.wechatEnabled || state.session.authenticated) {
    return;
  }

  wechatLoginBtn.disabled = true;
  wechatLoginBtn.classList.remove('is-ready');
  wechatLoginBtn.classList.add('is-checking');
  wechatLoginBtn.setAttribute('aria-label', '正在前往微信扫码登录');
  wechatLoginStatus.textContent = '跳转中';
  setWeChatLoginFeedback('正在打开微信扫码登录…');
  window.location.assign('/api/auth/wechat/start');
});

wechatLogoutBtn.addEventListener('click', async () => {
  if (!state.session || !state.session.authenticated) {
    return;
  }

  setWeChatLogoutBusy(true);
  setWeChatLoginFeedback('正在退出微信登录…');

  try {
    const response = await fetch('/api/auth/logout', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    if (!response.ok) {
      const data = await response.json().catch(() => ({}));
      const message = typeof data.error === 'string' && data.error.trim()
        ? data.error.trim()
        : '退出微信登录失败，请稍后重试。';
      throw new Error(message);
    }

    let session;
    try {
      session = await ensureSession();
    } catch {
      throw new Error('已退出微信登录，但状态刷新失败，请刷新页面。');
    }
    if (session.authenticated) {
      throw new Error('退出状态尚未生效，请稍后重试。');
    }
    setWeChatLoginFeedback('已退出微信登录，可扫码登录其他微信账号。', 'success');
  } catch (error) {
    setWeChatLoginFeedback(error instanceof Error && error.message ? error.message : '退出微信登录失败，请稍后重试。', 'error');
    setWeChatLogoutBusy(false);
  }
});

window.addEventListener('pageshow', (event) => {
  if (event.persisted && state.session) {
    renderWeChatLoginState(state.session);
  }
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
    window.clearTimeout(state.pendingSendTimer);
    state.pendingUserMessages = [];
    state.pendingUserRows = [];
    clearInlineReadStatus();
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
  const wechatLoginCallback = consumeWeChatLoginCallback();
  const initialView = wechatLoginCallback ? 'profile' : 'messages';
  let wechatLoginCallbackHandled = false;

  try {
    const session = await ensureSession();
    if (wechatLoginCallback) {
      setMainView(initialView);
      applyWeChatLoginCallback(wechatLoginCallback, session);
      wechatLoginCallbackHandled = true;
    }
    const [character, messages, moments] = await Promise.all([fetchCharacter(), fetchMessages(), fetchMoments()]);
    applyCharacterUI(character);
    state.messages = messages;
    renderMoments(moments);
    updateConversationSummary();
    setMainView(initialView);
    scheduleProactiveMessage(randomDelay(firstProactiveDelayRange));
    scheduleMomentCheck(randomDelay(momentCheckDelayRange));
  } catch (error) {
    setMainView(initialView);
    if (!state.session && wechatLoginCallback && !wechatLoginCallbackHandled) {
      setWeChatLoginFeedback('无法确认微信登录状态，请刷新页面后重试。', 'error');
    }
    conversationPreview.textContent = `初始化失败：${error.message}`;
  }
})();
