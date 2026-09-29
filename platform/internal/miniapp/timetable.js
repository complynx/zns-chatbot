import { browserSignIn, miniAppURL } from './browser-auth.js';
const $ = (selector) => document.querySelector(selector);
const launch = new URLSearchParams(location.hash.slice(1));
const initData = launch.get('tgWebAppData') || '';
const event = new URL(location.href).searchParams.get('event') || '';
const labels = new Map([
  ['title', ['Расписание массажа', 'Massage timetable']],
  ['language', ['Язык', 'Language']],
  ['party', ['Вечеринка', 'Party']],
  ['mine', ['Только мои', 'Only mine']],
  ['refresh', ['Обновить', 'Refresh']],
  ['zoomOut', ['Уменьшить', 'Zoom out']],
  ['zoomIn', ['Увеличить', 'Zoom in']],
  ['timezone', ['Время Минска (UTC+3).', 'Times are shown in Minsk (UTC+3).']],
  [
    'legend',
    [
      'Заливка: рабочие часы. В записи указаны клиент, время, длительность и цена.',
      'Shaded areas: working hours. Bookings show client, time, duration and price.',
    ],
  ],
  ['loading', ['Загрузка…', 'Loading…']],
  [
    'auth',
    [
      'Откройте расписание заново кнопкой в Telegram.',
      'Reopen the timetable using its Telegram button.',
    ],
  ],
  [
    'forbidden',
    [
      'Расписание доступно только специалистам и администраторам события.',
      'Only event specialists and administrators can view this timetable.',
    ],
  ],
  [
    'failed',
    [
      'Не удалось загрузить расписание. Нажмите «Обновить».',
      'Could not load the timetable. Press Refresh.',
    ],
  ],
  [
    'empty',
    ['Нет расписания для выбранной вечеринки.', 'No timetable for this party.'],
  ],
  [
    'noMine',
    [
      'У вас нет рабочего времени или записей на этой вечеринке.',
      'You have no working hours or bookings for this party.',
    ],
  ],
  ['time', ['Время', 'Time']],
  ['minutes', ['мин', 'min']],
  ['working', ['Рабочее время', 'Working hours']],
  ['now', ['Сейчас', 'Now']],
]);
const state = {
  data: undefined,
  busy: false,
  status: '',
  zoom: 1,
  renderedParty: '',
};

function text(key) {
  return labels.get(key)?.at($('#language').value === 'ru' ? 0 : 1) || key;
}

function element(tag, content = '', className = '') {
  const node = document.createElement(tag);
  node.textContent = content;
  node.className = className;
  return node;
}

function instant(value) {
  return new Date(value).getTime();
}

function format(value, hasDate = false) {
  return new Intl.DateTimeFormat($('#language').value, {
    timeZone: 'Europe/Minsk',
    ...(hasDate && { day: 'numeric', month: 'short' }),
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(value));
}

function setStatus(key) {
  state.status = key;
  $('#status').textContent = key ? text(key) : '';
}

function translate() {
  document.documentElement.lang = $('#language').value;
  document.title = text('title');
  for (const node of document.querySelectorAll('[data-label]')) {
    node.textContent = text(node.dataset.label);
  }
  $('#timetable').ariaLabel = text('title');
  setStatus(state.status);
}

function parties() {
  const selected = $('#party').value;
  $('#party').replaceChildren();
  const values = state.data?.calendar.parties || [];
  for (const party of values) {
    const option = element(
      'option',
      `${format(party.start, true)} — ${format(party.end, true)}`,
    );
    option.value = party.id;
    $('#party').append(option);
  }
  const current = values.find(
    (party) =>
      Date.now() >= instant(party.start) - 7_200_000 &&
      Date.now() <= instant(party.end) + 7_200_000,
  );
  if (values.some((party) => party.id === selected))
    $('#party').value = selected;
  else if (current) $('#party').value = current.id;
  $('#party').disabled = state.busy || values.length === 0;
}

function bounds(party) {
  return {
    start: instant(party.start) - 7_200_000,
    end: instant(party.end) + 7_200_000,
  };
}

function hasOverlap(span, range) {
  return instant(span.start) < range.end && instant(span.end) > range.start;
}

function position(node, span, range, height) {
  const start = Math.max(range.start, instant(span.start));
  const end = Math.min(range.end, instant(span.end));
  node.style.top = `${((start - range.start) / (range.end - range.start)) * height}px`;
  node.style.height = `${Math.max(1, ((end - start) / (range.end - range.start)) * height)}px`;
}

function column(grid, title, height, isAxis = false) {
  const wrapper = element('section', '', isAxis ? 'column axis' : 'column');
  wrapper.append(element('h2', title, 'provider'));
  const timeline = element('div', '', 'timeline');
  timeline.style.height = `${height}px`;
  wrapper.append(timeline);
  grid.append(wrapper);
  return timeline;
}

function bookingText(booking, clients) {
  const duration = Math.round(
    (instant(booking.end) - instant(booking.start)) / 60_000,
  );
  return `${clients.get(booking.owner) || booking.owner}\n${format(booking.start)}–${format(booking.end)}\n${duration} ${text('minutes')} · ${booking.price} BYN / ${booking.price_rub} RUB`;
}

function renderProvider(grid, provider, bookings, clients, range, height) {
  const timeline = column(
    grid,
    `${provider.icon || ''} ${provider.name}`.trim(),
    height,
  );
  const working = provider.work || [];
  for (const span of working) {
    if (!hasOverlap(span, range)) continue;
    const work = element('div', '', 'working');
    work.title = `${text('working')}: ${format(span.start)}–${format(span.end)}`;
    work.setAttribute('role', 'img');
    work.setAttribute('aria-label', work.title);
    position(work, span, range, height);
    timeline.append(work);
  }
  for (const booking of bookings) {
    if (booking.specialist !== provider.owner) continue;
    const node = element('div', bookingText(booking, clients), 'booking');
    node.style.whiteSpace = 'pre-line';
    node.tabIndex = 0;
    node.title = node.textContent;
    position(node, booking, range, height);
    timeline.append(node);
  }
  if (Date.now() < range.start || Date.now() > range.end) return;
  const now = element('div', '', 'now');
  now.title = text('now');
  now.style.top = `${((Date.now() - range.start) / (range.end - range.start)) * height}px`;
  timeline.append(now);
}

function viewPosition() {
  return {
    party: state.renderedParty,
    height: $('#timetable .timeline')?.offsetHeight || 0,
    top: $('#timetable').scrollTop,
    left: $('#timetable').scrollLeft,
  };
}

function render(previous = viewPosition()) {
  $('#timetable').replaceChildren();
  state.renderedParty = '';
  $('#legend').hidden = true;
  const party = state.data?.calendar.parties?.find(
    (item) => item.id === $('#party').value,
  );
  $('#mine').disabled = state.busy || !state.data;
  $('#zoom-in').disabled = !party || state.zoom >= 3;
  $('#zoom-out').disabled = !party || state.zoom <= 1;
  if (!state.data) return;
  if (!party) {
    setStatus('empty');
    return;
  }
  const range = bounds(party);
  const calendar = state.data.calendar;
  const bookings = (calendar.bookings || []).filter(
    (item) =>
      item.party === party.id && !item.cancelled_at && hasOverlap(item, range),
  );
  const providers = (calendar.providers || []).filter(
    (provider) =>
      (!$('#mine').checked || provider.owner === state.data.owner) &&
      ((provider.work || []).some((span) => hasOverlap(span, range)) ||
        bookings.some((item) => item.specialist === provider.owner)),
  );
  if (providers.length === 0) {
    setStatus($('#mine').checked ? 'noMine' : 'empty');
    return;
  }
  setStatus('');
  $('#legend').hidden = false;
  const height = ((range.end - range.start) / 3_600_000) * 320 * state.zoom;
  const grid = element('div', '', 'grid');
  const axis = column(grid, text('time'), height, true);
  for (let time = range.start; time < range.end; time += 1_800_000) {
    const tick = element('div', format(time), 'tick');
    tick.style.top = `${((time - range.start) / (range.end - range.start)) * height}px`;
    axis.append(tick);
  }
  const clients = new Map(
    (calendar.clients || []).map((client) => [client.owner, client.name]),
  );
  for (const provider of providers)
    renderProvider(grid, provider, bookings, clients, range, height);
  $('#timetable').append(grid);
  state.renderedParty = party.id;
  const initialTime =
    Date.now() >= instant(party.start) && Date.now() < range.end
      ? Date.now()
      : instant(party.start);
  $('#timetable').scrollTop =
    previous.party === party.id && previous.height > 0
      ? (previous.top * height) / previous.height
      : ((initialTime - range.start) / (range.end - range.start)) * height;
  $('#timetable').scrollLeft = previous.party === party.id ? previous.left : 0;
}

async function refresh() {
  if (state.busy) return;
  const previous = viewPosition();
  state.busy = true;
  state.data = undefined;
  $('#refresh').disabled = true;
  $('#party').disabled = true;
  render();
  setStatus('loading');
  try {
    const response = await fetch(
      miniAppURL(`api/massage/timetable?event=${encodeURIComponent(event)}`),
      {
        headers: initData ? { Authorization: `tma ${initData}` } : {},
        cache: 'no-store',
        signal: AbortSignal.timeout(15_000),
      },
    );
    if (!response.ok) {
      setStatus(
        response.status === 401
          ? 'auth'
          : response.status === 403
            ? 'forbidden'
            : 'failed',
      );
      return;
    }
    state.data = await response.json();
    parties();
    render(previous);
  } catch {
    setStatus('failed');
  } finally {
    state.busy = false;
    $('#refresh').disabled = false;
    $('#party').disabled = !state.data?.calendar.parties?.length;
    $('#mine').disabled = !state.data;
  }
}

$('#language').value =
  new URL(location.href).searchParams.get('lang') === 'ru' ? 'ru' : 'en';
$('#language').addEventListener('change', () => {
  translate();
  parties();
  render();
});
$('#party').addEventListener('change', () => render());
$('#mine').addEventListener('change', () => render());
$('#refresh').addEventListener('click', refresh);
$('#zoom-in').addEventListener('click', () => {
  state.zoom = Math.min(3, state.zoom + 0.5);
  render();
});
$('#zoom-out').addEventListener('click', () => {
  state.zoom = Math.max(1, state.zoom - 0.5);
  render();
});
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) void refresh();
});
setInterval(() => {
  if (!document.hidden) void refresh();
}, 60_000);
translate();
await browserSignIn(initData);
void refresh();
