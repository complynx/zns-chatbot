const $ = (s) => document.querySelector(s);
const view = { last: '', isPolling: false, messages: new Map() };
const stickerLabels = {
  download: 'Sticker artwork / Изображение стикера',
};
function stickerArtwork(sticker, user) {
  const link = document.createElement('a');
  link.href =
    '/lab/files/' + encodeURIComponent(sticker.file_id) + '?user=' + user;
  link.textContent = stickerLabels.download;
  if (!sticker.is_animated) {
    const artwork = document.createElement(sticker.is_video ? 'video' : 'img');
    artwork.src = link.href + '&preview=1';
    artwork.className = 'uploaded-photo';
    if (sticker.is_video) {
      artwork.controls = true;
      artwork.preload = 'none';
    } else artwork.alt = stickerLabels.download;
    link.replaceChildren(artwork);
  }
  return link;
}
function safeEntityURL(entity) {
  const target =
    entity.type === 'text_mention'
      ? 'tg://user?id=' + String(entity.user?.id || '')
      : entity.url;
  try {
    const parsed = new URL(target);
    if (parsed.username || parsed.password) return;
    if (['http:', 'https:'].includes(parsed.protocol) && parsed.hostname)
      return target;
    if (
      parsed.protocol === 'tg:' &&
      parsed.hostname === 'user' &&
      !parsed.pathname &&
      !parsed.hash &&
      /^\?id=[1-9]\d*$/.test(parsed.search)
    )
      return target;
  } catch {
    /*
    Unsupported entity targets remain literal text.
    */
  }
}
function entityElement(entity) {
  if (entity.type === 'text_link' || entity.type === 'text_mention') {
    const target = safeEntityURL(entity);
    if (!target) return;
    const link = document.createElement('a');
    link.href = target;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    return link;
  }
  if (entity.type === 'spoiler') {
    const spoiler = document.createElement('span');
    spoiler.className = 'spoiler';
    spoiler.title = 'Spoiler / Спойлер';
    return spoiler;
  }
  const tags = {
    bold: 'strong',
    italic: 'em',
    underline: 'u',
    strikethrough: 's',
    code: 'code',
    pre: 'pre',
    blockquote: 'blockquote',
  };
  const tag = tags[entity.type];
  if (tag) return document.createElement(tag);
}
function messageText(node, message, stickers, user) {
  const text = message.text || message.caption || '';
  const source = message.text ? message.entities : message.caption_entities;
  const entities = (source || []).filter(
    (entity) =>
      Number.isSafeInteger(entity.offset) &&
      Number.isSafeInteger(entity.length) &&
      entity.offset >= 0 &&
      entity.length > 0 &&
      entity.offset + entity.length <= text.length,
  );
  const boundaries = new Set([0, text.length]);
  for (const entity of entities) {
    boundaries.add(entity.offset);
    boundaries.add(entity.offset + entity.length);
  }
  const positions = [...boundaries].toSorted((a, b) => a - b);
  const wrappers = [];
  for (let index = 0; index + 1 < positions.length; index++) {
    const start = positions.at(index);
    const end = positions.at(index + 1);
    const active = entities.filter(
      (entity) =>
        entity.offset <= start && entity.offset + entity.length >= end,
    );
    const emoji = active.find(
      (entity) =>
        entity.type === 'custom_emoji' && stickers[entity.custom_emoji_id],
    );
    if (emoji && emoji.offset !== start) continue;
    let content;
    if (emoji) {
      content = stickerArtwork(stickers[emoji.custom_emoji_id], user);
      content.className = 'custom-emoji';
    } else content = document.createTextNode(text.slice(start, end));
    // Keep block containers open across changes to inline formatting.
    const ordered = active.toSorted((a, b) => {
      const aBlock = Number(['blockquote', 'pre'].includes(a.type));
      const bBlock = Number(['blockquote', 'pre'].includes(b.type));
      return bBlock - aBlock || a.offset - b.offset || b.length - a.length;
    });
    let common = 0;
    while (
      common < wrappers.length &&
      wrappers.at(common).entity === ordered.at(common)
    ) {
      common++;
    }
    wrappers.length = common;
    for (const entity of ordered.slice(common)) {
      const wrapper = entityElement(entity);
      if (!wrapper) continue;
      (wrappers.at(-1)?.node || node).append(wrapper);
      wrappers.push({ entity, node: wrapper });
    }
    (wrappers.at(-1)?.node || node).append(content);
  }
}
async function post(path, body) {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Sandbox': '1' },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error('Запрос отклонён: ' + r.status);
  return r.json();
}
async function send(text, data = '', message_id = 0, metadata = {}) {
  try {
    $('#status').textContent = '';
    await post('/lab/input', {
      ...metadata,
      user: Number($('#user').value),
      username: $('#telegram-username').value,
      text,
      data,
      message_id,
    });
    await refresh();
  } catch (error) {
    $('#status').textContent = error.message;
  }
}
async function openMiniApp(button, messageId) {
  const user = $('#user').value;
  try {
    const launch = await post('/lab/webapp', {
      user: Number(user),
      message_id: messageId,
      url: button.web_app.url,
    });
    if (user !== $('#user').value) return;
    $('#miniapp-frame').title = button.text;
    $('#miniapp-dialog').setAttribute('aria-label', button.text);
    $('#miniapp-frame').src = launch.url;
    $('#miniapp-dialog').showModal();
  } catch (error) {
    $('#status').textContent = error.message;
  }
}
async function refresh() {
  if (view.isPolling) return;
  view.isPolling = true;
  try {
    const user = $('#user').value;
    const r = await fetch('/lab/state?user=' + user);
    if (!r.ok) throw new Error('Стенд ещё не готов: ' + r.status);
    const s = await r.json();
    if (user !== $('#user').value) return;
    const encoded = JSON.stringify(s.messages);
    if (encoded !== view.last) {
      view.last = encoded;
      const visible = new Set(s.messages.map((m) => m.message_id));
      for (const [id, rendered] of view.messages) {
        if (visible.has(id)) continue;
        rendered.node.remove();
        view.messages.delete(id);
      }
      for (const m of s.messages) {
        const serialized = JSON.stringify(m);
        const previous = view.messages.get(m.message_id);
        if (previous?.serialized === serialized) continue;
        const box = document.createElement('div');
        box.className = 'message';
        box.dataset.bot = String(m.from.is_bot);
        box.dataset.messageId = String(m.message_id);
        const sender = document.createElement('small');
        sender.textContent =
          (m.from.is_bot ? 'Бот' : 'Вы') +
          (m.message_thread_id ? ' · Topic / Тема ' + m.message_thread_id : '');
        box.append(sender);
        const p = document.createElement('p');
        messageText(p, m, s.stickers || {}, user);
        box.append(p);
        if (m.contact || m.forward_origin) {
          const contact = document.createElement('p');
          contact.textContent = m.contact
            ? 'Contact / Контакт: ' + m.contact.first_name
            : 'Forward / Пересылка: ' +
              (m.forward_origin.sender_user?.first_name ||
                'Hidden sender / Скрытый отправитель');
          box.append(contact);
        }
        if (m.sticker) box.append(stickerArtwork(m.sticker, user));
        if (m.document) {
          const file = document.createElement('a');
          file.textContent = 'Документ: ' + m.document.file_name;
          file.href =
            '/lab/files/' +
            encodeURIComponent(m.document.file_id) +
            '?user=' +
            user;
          file.download = m.document.file_name;
          box.append(file);
        }
        if (m.photo?.length) {
          let photo = m.photo[0];
          for (const item of m.photo) {
            if (item.width * item.height > photo.width * photo.height)
              photo = item;
          }
          const image = document.createElement('img');
          image.src =
            '/lab/files/' + encodeURIComponent(photo.file_id) + '?user=' + user;
          image.alt = 'Photo / Фото';
          image.className = 'uploaded-photo';
          const link = document.createElement('a');
          link.href = image.src;
          link.download = 'photo';
          link.append(image);
          box.append(link);
        }
        for (const [kind, media] of [
          ['audio', m.audio],
          ['voice', m.voice],
          ['video', m.video],
          ['video_note', m.video_note],
        ]) {
          if (!media) continue;
          const link = document.createElement('a');
          link.textContent = `${kind}: ${media.file_name || media.file_id} (${media.duration}s declared)`;
          link.href =
            '/lab/files/' + encodeURIComponent(media.file_id) + '?user=' + user;
          link.download = media.file_name || kind;
          box.append(link);
          const player = document.createElement(
            kind === 'audio' || kind === 'voice' ? 'audio' : 'video',
          );
          player.controls = true;
          player.preload = 'none';
          player.src = link.href + '&preview=1';
          player.className = 'uploaded-photo';
          box.append(player);
        }
        const buttons = document.createElement('div');
        buttons.className = 'buttons';
        const keyboard = m.reply_markup.inline_keyboard || [];
        for (const row of keyboard)
          for (const b of row) {
            const element = document.createElement('button');
            element.textContent = b.text;
            element.disabled = Number(user) < 0;
            element.addEventListener('click', () => {
              if (b.url) {
                $('#status').textContent =
                  'Тестовый переход к контакту: ' + b.url;
              } else if (b.web_app) openMiniApp(b, m.message_id);
              else send('', b.callback_data, m.message_id);
            });
            buttons.append(element);
          }
        box.append(buttons);
        if (previous) previous.node.replaceWith(box);
        else $('#messages').append(box);
        view.messages.set(m.message_id, { serialized, node: box });
      }
    }
    $('#history').textContent = JSON.stringify(s.history, undefined, 2);
    $('#edits').textContent = s.edits;
  } catch (error) {
    $('#status').textContent = error.message;
  } finally {
    view.isPolling = false;
  }
}
$('#chat').addEventListener('submit', (event) => {
  event.preventDefault();
  const text = $('#text').value;
  $('#text').value = '';
  send(text);
});
$('#start').addEventListener('click', () => send('/start'));
$('#contact-form').addEventListener('submit', (event) => {
  event.preventDefault();
  const mode = $('#contact-mode').value;
  const person = Number($('#contact-user').value);
  const metadata =
    mode === 'contact'
      ? { contact_id: person }
      : { forward_user_id: person, hidden_sender: mode === 'hidden' };
  send($('#contact-text').value, '', 0, metadata);
});
$('#document-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const file = $('#document').files[0];
  if (!file) return;
  const user = $('#user').value;
  $('#send-document').disabled = true;
  try {
    const query = new URLSearchParams({
      user,
      filename: file.name,
      caption: $('#caption').value,
      duration: $('#duration').value,
      marker: $('#emoji-marker').value,
    });
    const route = '/lab/' + encodeURIComponent($('#upload-kind').value) + '?';
    const response = await fetch(route + query, {
      method: 'POST',
      headers: { 'X-Sandbox': '1' },
      body: file,
    });
    if (!response.ok) throw new Error('Документ отклонён: ' + response.status);
    await response.json();
    $('#document').value = '';
    $('#caption').value = '';
    $('#status').textContent = '';
    await refresh();
  } catch (error) {
    $('#status').textContent = error.message;
  } finally {
    $('#send-document').disabled = false;
  }
});
$('#orders').addEventListener('click', () => send('/orders'));
$('#user').addEventListener('change', () => {
  $('#telegram-username').value = '';
  $('#miniapp-dialog').close();
  const isReadOnly = Number($('#user').value) < 0;
  for (const control of document.querySelectorAll(
    '#chat input, #chat button, #contact-form input, #contact-form select, #contact-form button, #document-form input, #document-form select, #document-form button, #start, #orders',
  )) {
    control.disabled = isReadOnly;
  }
  $('#status').textContent = isReadOnly
    ? 'Delivery view / Просмотр доставки'
    : '';
  view.last = '';
  view.messages.clear();
  $('#messages').replaceChildren();
  refresh();
});
$('#miniapp-close').addEventListener('click', () =>
  $('#miniapp-dialog').close(),
);
$('#miniapp-dialog').addEventListener('close', () => {
  $('#miniapp-frame').removeAttribute('src');
  refresh();
});
for (const button of document.querySelectorAll('[data-fault]')) {
  button.addEventListener('click', async () => {
    try {
      await post('/lab/fault', { mode: button.dataset.fault });
      $('#status').textContent = 'Сбой включён для следующего запроса.';
    } catch (error) {
      $('#status').textContent = error.message;
    }
  });
}
setInterval(refresh, 700);
await refresh();
