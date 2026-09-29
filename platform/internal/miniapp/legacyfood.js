import { browserSignIn, miniAppURL } from './browser-auth.js';

const $ = (selector) => document.querySelector(selector);
const parameters = new URL(location.href).searchParams;
const initData =
  new URLSearchParams(location.hash.slice(1)).get('tgWebAppData') || '';
const labels = new Map([
  ['details', ['Ingredients and nutrition', 'Состав и пищевая ценность']],
  ['calories', ['kcal', 'ккал']],
  ['protein', ['Protein', 'Белки']],
  ['fat', ['Fat', 'Жиры']],
  ['carbohydrates', ['Carbohydrates', 'Углеводы']],
  ['grams', ['g', 'г']],
  ['title', ['Meals', 'Питание']],
  ['language', ['Language', 'Язык']],
  ['save', ['Save', 'Сохранить']],
  ['reload', ['Reload', 'Обновить']],
  ['loading', ['Loading…', 'Загрузка…']],
  ['saved', ['Saved.', 'Сохранено.']],
  [
    'failed',
    [
      'Could not save or load. Reload and retry.',
      'Не удалось сохранить или загрузить. Обновите и повторите.',
    ],
  ],
  [
    'isLocked',
    [
      'Meal payment is submitted or confirmed. Choices are locked.',
      'Оплата питания отправлена или подтверждена. Выбор защищён от изменений.',
    ],
  ],
  [
    'incomplete',
    ['Choose lunch for each day.', 'Выберите обед на каждый день.'],
  ],
  ['lunch', ['Lunch', 'Обед']],
  ['dinner', ['Dinner', 'Ужин']],
  ['choose', ['Choose…', 'Выберите…']],
  ['no-lunch', ['No lunch', 'Без обеда']],
  ['individual-items', ['Individual dishes', 'Отдельные блюда']],
  ['combo-with-soup', ['Combo with soup', 'Комбо с супом']],
  ['combo-no-soup', ['Combo without soup', 'Комбо без супа']],
  ['soup', ['Soup', 'Суп']],
  ['main', ['Main', 'Основное']],
  ['side', ['Side', 'Гарнир']],
  ['salad', ['Salad', 'Салат']],
  ['friday', ['Friday', 'Пятница']],
  ['saturday', ['Saturday', 'Суббота']],
  ['sunday', ['Sunday', 'Воскресенье']],
  ['total', ['Total', 'Итого']],
  [
    'stale',
    [
      'Choices changed. Reload before saving again.',
      'Выбор изменился. Обновите перед повторным сохранением.',
    ],
  ],
]);
const state = {
  view: undefined,
  meals: {},
  isBusy: false,
  quoteSequence: 0,
  saveAttempt: undefined,
  statusKey: '',
};
const text = (key) =>
  labels.get(key)?.[$('#language').value === 'ru' ? 1 : 0] || key;
const money = (value) =>
  new Intl.NumberFormat($('#language').value, {
    style: 'currency',
    currency: 'RUB',
  }).format(value);
const isLocked = () =>
  ['paid', 'proof_submitted'].includes(state.view?.order.meal_payment.status) ||
  !state.view?.event.active;
function element(tag, value = '') {
  const node = document.createElement(tag);
  node.textContent = value;
  return node;
}
function status(key) {
  state.statusKey = key;
  $('#status').textContent = text(key);
}
function translate() {
  document.documentElement.lang = $('#language').value;
  document.title = text('title');
  for (const node of document.querySelectorAll('[data-text]')) {
    node.textContent = text(node.dataset.text);
  }
  status(state.statusKey);
}
async function request(path, body) {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    cache: 'no-store',
    headers: {
      ...(initData && { Authorization: 'tma ' + initData }),
      'Content-Type': 'application/json',
    },
    ...(body !== undefined && { body: JSON.stringify(body) }),
    signal: AbortSignal.timeout(15_000),
  });
  const result = await response.json();
  if (!response.ok) throw new Error(result.code || 'failed');
  return result;
}
function option(value, caption) {
  const item = element('option', caption);
  item.value = value;
  return item;
}
function dishTitle(dish) {
  return $('#language').value === 'ru' ? dish.title_ru : dish.title_en;
}
function dishDetails(dish) {
  const details = element('details');
  details.append(element('summary', text('details')));
  const ingredients =
    $('#language').value === 'ru' ? dish.ingredients_ru : dish.ingredients_en;
  if (ingredients) details.append(element('p', ingredients));
  if (dish.weight?.value !== undefined && dish.weight.value !== null)
    details.append(
      element('p', `${dish.weight.value} ${dish.weight.unit || ''}`),
    );
  if (dish.nutrition) {
    for (const [key, value] of Object.entries(dish.nutrition)) {
      if (typeof value === 'number')
        details.append(
          element(
            'p',
            `${text(key)}: ${value}${key === 'calories' ? '' : ' ' + text('grams')}`,
          ),
        );
    }
  }
  if (dish.photo) {
    const image = document.createElement('img');
    if (/^[a-z][a-z0-9_]*$/.test(dish.photo))
      image.src = miniAppURL('foodphotos/' + dish.photo + '.jpg');
    else if (dish.photo.startsWith('https://')) image.src = dish.photo;
    if (image.src) {
      image.alt = dishTitle(dish);
      image.loading = 'lazy';
      details.append(image);
    }
  }
  return details;
}
function checkboxes(parent, dishes, values, changed) {
  for (const [index, dish] of dishes.entries()) {
    const label = element('label');
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = values.some((value) => Number(value) === index);
    input.addEventListener('change', () => {
      changed(index, input.checked);
      state.saveAttempt = undefined;
      void quote();
    });
    label.append(
      input,
      document.createTextNode(`${dishTitle(dish)} — ${money(dish.price)}`),
    );
    parent.append(label);
    parent.append(dishDetails(dish));
  }
}
function combo(parent, dishes, lunch) {
  const categories =
    lunch.type === 'combo-with-soup'
      ? ['soup', 'main', 'side', 'salad']
      : ['main', 'side', 'salad'];
  for (const category of categories) {
    const label = element('label', text(category));
    const select = document.createElement('select');
    select.append(option('', text('choose')));
    for (const [index, dish] of dishes.entries()) {
      if (dish.category === category)
        select.append(option(String(index), dishTitle(dish)));
    }
    const chosen = new Map(Object.entries(lunch.items || {})).get(
      category + '_index',
    );
    select.value =
      typeof chosen !== 'number' && typeof chosen !== 'string'
        ? ''
        : String(chosen);
    select.addEventListener('change', () => {
      lunch.items ||= {};
      Object.defineProperty(lunch.items, category + '_index', {
        value: select.value === '' ? undefined : Number(select.value),
        enumerable: true,
        writable: true,
        configurable: true,
      });
      state.saveAttempt = undefined;
      void quote();
      showDetails();
    });
    const preview = element('div');
    function showDetails() {
      const dish = dishes.at(Number(select.value));
      preview.replaceChildren(
        ...(dish && select.value !== '' ? [dishDetails(dish)] : []),
      );
    }
    showDetails();
    label.append(select);
    parent.append(label);
    parent.append(preview);
  }
}
function renderDay(day, catalog) {
  const section = element('section');
  section.append(element('h2', text(day)));
  const fieldset = document.createElement('fieldset');
  fieldset.disabled = isLocked() || state.isBusy;
  const selection = new Map(Object.entries(state.meals)).get(day) || {};
  Object.defineProperty(state.meals, day, {
    value: selection,
    enumerable: true,
    writable: true,
    configurable: true,
  });
  if (catalog.lunch?.length > 0) renderLunch(fieldset, catalog, selection);
  if (catalog.dinner?.length > 0) {
    fieldset.append(element('h3', text('dinner')));
    selection.dinner ||= [];
    checkboxes(fieldset, catalog.dinner, selection.dinner, (index, checked) => {
      selection.dinner = selection.dinner.filter(
        (value) => Number(value) !== index,
      );
      if (checked) selection.dinner.push(index);
    });
  }
  section.append(fieldset);
  return section;
}
function render() {
  if (!state.view) return;
  $('#days').replaceChildren(
    ...Object.entries(state.view.event.menu).map(([day, catalog]) =>
      renderDay(day, catalog),
    ),
  );
  $('#payment').textContent = isLocked() ? text('isLocked') : '';
  $('#menu button[type="submit"]').disabled = state.isBusy || isLocked();
  $('#reload').disabled = state.isBusy;
  $('#menu').hidden = false;
}
async function quote() {
  if (!state.view) return;
  if (isLocked()) {
    $('#total').textContent =
      `${text('total')}: ${money(state.view.order.meal_total)}`;
    return;
  }
  const sequence = ++state.quoteSequence;
  try {
    const result = await request(miniAppURL('api/food/quote'), {
      event_id: state.view.event.id,
      meals: state.meals,
    });
    if (sequence !== state.quoteSequence) return;
    $('#total').textContent =
      `${text('total')}: ${money(result.total)}${result.complete ? '' : ' · ' + text('incomplete')}`;
  } catch {
    if (sequence === state.quoteSequence) status('failed');
  }
}
async function load() {
  state.isBusy = true;
  render();
  status('loading');
  try {
    const query = new URLSearchParams({
      pass_key: parameters.get('pass_key') || '',
      order_id: parameters.get('order_id') || '',
    });
    state.view = await request(miniAppURL('api/food?' + query));
    state.meals = structuredClone(state.view.order.meals);
    state.saveAttempt = undefined;
    status('');
  } catch {
    status('failed');
  } finally {
    state.isBusy = false;
    render();
  }
  await quote();
}
$('#menu').addEventListener('submit', async (event) => {
  event.preventDefault();
  if (state.isBusy || isLocked()) return;
  state.saveAttempt ||= {
    event_id: state.view.event.id,
    order_id: state.view.order.id,
    version: state.view.order.version,
    key: crypto.randomUUID(),
    name: 'save_meals',
    meals: structuredClone(state.meals),
  };
  state.isBusy = true;
  render();
  try {
    state.view.order = await request(miniAppURL('api/food'), state.saveAttempt);
    state.meals = structuredClone(state.view.order.meals);
    state.saveAttempt = undefined;
    status('saved');
  } catch (error) {
    status(error.message === 'stale_version' ? 'stale' : 'failed');
  } finally {
    state.isBusy = false;
    render();
  }
});
$('#language').value = parameters.get('lang') === 'ru' ? 'ru' : 'en';
$('#language').addEventListener('change', () => {
  translate();
  render();
  void quote();
});
$('#reload').addEventListener('click', load);
translate();
await browserSignIn(initData);
await load();

function renderLunch(fieldset, catalog, selection) {
  const label = element('label', text('lunch'));
  const type = document.createElement('select');
  type.append(option('', text('choose')));
  for (const kind of [
    'no-lunch',
    'individual-items',
    'combo-with-soup',
    'combo-no-soup',
  ]) {
    const price =
      kind === 'combo-with-soup'
        ? state.view.event.meal_prices.with_soup
        : kind === 'combo-no-soup'
          ? state.view.event.meal_prices.without_soup
          : undefined;
    type.append(
      option(
        kind,
        text(kind) + (price === undefined ? '' : ` — ${money(price)}`),
      ),
    );
  }
  type.value = selection.lunch?.type || '';
  type.addEventListener('change', () => {
    selection.lunch = type.value
      ? {
          type: type.value,
          ...(type.value === 'individual-items' && { items: [] }),
          ...(type.value.startsWith('combo-') && { items: {} }),
        }
      : undefined;
    state.saveAttempt = undefined;
    render();
    void quote();
  });
  label.append(type);
  fieldset.append(label);
  if (selection.lunch?.type === 'individual-items') {
    selection.lunch.items ||= [];
    checkboxes(
      fieldset,
      catalog.lunch,
      selection.lunch.items,
      (index, checked) => {
        selection.lunch.items = selection.lunch.items.filter(
          (value) => Number(value) !== index,
        );
        if (checked) selection.lunch.items.push(index);
      },
    );
  } else if (selection.lunch?.type.startsWith('combo-'))
    combo(fieldset, catalog.lunch, selection.lunch);
}
