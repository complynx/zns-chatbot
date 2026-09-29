// Resolve against this served module so legacy entry routes share the same mount.
export function miniAppURL(path) {
  return new URL(path, import.meta.url);
}

const labels = new Map([
  ['username', ['Имя пользователя Telegram', 'Telegram username']],
  ['start', ['Войти через Telegram', 'Sign in through Telegram']],
  ['cancel', ['Отменить', 'Cancel']],
  ['logout', ['Выйти', 'Sign out']],
  [
    'pending',
    [
      'Подтвердите запрос в личном чате с ботом. Код: ',
      'Approve the request in your private bot chat. Code: ',
    ],
  ],
  [
    'declined',
    ['Вход отклонён. Можно повторить.', 'Sign-in declined. You can retry.'],
  ],
  ['cancelled', ['Вход отменён.', 'Sign-in cancelled.']],
  [
    'expired',
    ['Запрос истёк. Начните заново.', 'Request expired. Start again.'],
  ],
  [
    'unavailable',
    [
      'Не удалось войти. Проверьте имя пользователя, откройте личный чат с ботом и повторите.',
      'Sign-in unavailable. Check your username, open your private bot chat and retry.',
    ],
  ],
  [
    'rate_limited',
    [
      'Слишком много запросов. Повторите через 5 минут.',
      'Too many requests. Retry in 5 minutes.',
    ],
  ],
]);

async function authRequest(path, body) {
  const response = await fetch(miniAppURL('auth/' + path), {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'X-Browser-Auth': '1', 'Content-Type': 'application/json' },
    ...(body !== undefined && { body: JSON.stringify(body) }),
    cache: 'no-store',
    signal: AbortSignal.timeout(15_000),
  });
  const value = await response.json();
  if (!response.ok) throw new Error(value.code || 'unavailable');
  return value;
}

function element(tag, parent) {
  const node = document.createElement(tag);
  parent.append(node);
  return node;
}

// Both browser entry pages share the same consent UI and HttpOnly session.
export async function browserSignIn(initData) {
  if (initData) return;
  const language = document.querySelector('#language');
  const text = (key) =>
    labels.get(key)?.at(language.value === 'ru' ? 0 : 1) || '';
  const form = document.createElement('form');
  form.id = 'browser-auth';
  document.querySelector('header').after(form);
  const label = element('label', form);
  const caption = element('span', label);
  const input = element('input', label);
  input.name = 'username';
  input.autocomplete = 'username';
  input.maxLength = 64;
  input.required = true;
  const start = element('button', form);
  start.type = 'submit';
  const cancel = element('button', form);
  cancel.type = 'button';
  const status = element('p', form);
  status.setAttribute('role', 'status');
  let request = sessionStorage.getItem('browser-auth-request') || '';
  let state = request ? 'pending' : '';
  let isBusy = true;
  const render = () => {
    caption.textContent = text('username');
    start.textContent = text('start');
    cancel.textContent = text('cancel');
    start.disabled = isBusy || Boolean(request);
    cancel.hidden = !request;
    status.textContent =
      text(state) + (state === 'pending' ? request.slice(0, 8) : '');
  };
  language.addEventListener('change', render);
  render();

  const { promise: authorized, resolve: finish } = Promise.withResolvers();
  const complete = () => {
    request = '';
    sessionStorage.removeItem('browser-auth-request');
    form.remove();
    language.removeEventListener('change', render);
    const logout = document.createElement('button');
    logout.type = 'button';
    logout.textContent = text('logout');
    document.querySelector('header').append(logout);
    language.addEventListener('change', () => {
      logout.textContent = text('logout');
    });
    logout.addEventListener('click', async () => {
      logout.disabled = true;
      try {
        await authRequest('logout', {});
        location.reload();
      } catch {
        logout.disabled = false;
      }
    });
    finish();
  };
  const clear = () => {
    request = '';
    sessionStorage.removeItem('browser-auth-request');
  };
  async function poll() {
    const current = request;
    if (!current) return;
    try {
      const value = await authRequest(
        'status?request=' + encodeURIComponent(current),
      );
      if (request !== current) return;
      if (value.result === 'approved') {
        complete();
        return;
      }
      state = value.result;
      if (state !== 'pending') clear();
    } catch (error) {
      if (request !== current) return;
      if (error.message === 'unauthorized') clear();
      state = 'unavailable';
    }
    render();
    if (request)
      setTimeout(() => {
        void poll();
      }, 2000);
  }
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (isBusy || request) return;
    isBusy = true;
    render();
    try {
      const value = await authRequest('start', { username: input.value });
      request = value.request;
      state = 'pending';
      sessionStorage.setItem('browser-auth-request', request);
      void poll();
    } catch (error) {
      state = error.message === 'rate_limited' ? 'rate_limited' : 'unavailable';
    } finally {
      isBusy = false;
      render();
    }
  });
  cancel.addEventListener('click', async () => {
    const current = request;
    cancel.disabled = true;
    try {
      await authRequest('cancel?request=' + encodeURIComponent(current), {});
      clear();
      state = 'cancelled';
    } catch {
      state = 'unavailable';
    } finally {
      cancel.disabled = false;
      render();
    }
  });
  try {
    await authRequest('check');
    complete();
  } catch {
    isBusy = false;
    render();
    if (request) void poll();
  }
  return authorized;
}
