import { browserSignIn, miniAppURL } from './browser-auth.js';
// Telegram's client passes signed initData in tgWebAppData. The server verifies
// it on every API request. No identity is derived from an order URL or form.
const $ = (selector) => document.querySelector(selector);
const launch = new URLSearchParams(location.hash.slice(1));
const initData = launch.get('tgWebAppData') || '';
const orderId = new URL(location.href).searchParams.get('order_id') || '';
const orderPath = miniAppURL('api/orders/' + encodeURIComponent(orderId));
const state = {
  data: undefined,
  isBusy: false,
  quoteInput: '',
  pending: undefined,
  names: new Map(),
  extras: new Map(),
  meals: new Map(),
};
const labels = new Map([
  ['title', ['Питание и услуги', 'Meals and services']],
  ['customer', ['Для кого заказ', 'Customer']],
  ['first', ['Имя', 'First name']],
  ['last', ['Фамилия', 'Last name']],
  ['middle', ['Отчество (необязательно)', 'Middle name (optional)']],
  ['extras', ['Услуги', 'Services']],
  ['quote', ['Рассчитать', 'Calculate']],
  ['save', ['Сохранить', 'Save']],
  ['reload', ['Загрузить актуальный заказ', 'Load current order']],
  ['total', ['Итого', 'Total']],
  ['service', ['Упаковка и приборы', 'Packaging and utensils']],
  [
    'saved',
    [
      'Заказ сохранён. Карточка в чате обновится.',
      'Order saved. The chat card will update.',
    ],
  ],
  [
    'readonly',
    ['Заказ доступен только для просмотра.', 'This order is read-only.'],
  ],
  [
    'changed',
    [
      'Состав изменён. Нажмите «Рассчитать».',
      'Selection changed. Press Calculate.',
    ],
  ],
  [
    'conflict',
    [
      'Заказ изменился. Ваш черновик сохранён в форме. Загрузите актуальный заказ перед новым редактированием.',
      'Order changed. Your draft remains in the form. Load the current order before editing again.',
    ],
  ],
  [
    'retry',
    [
      'Не удалось подтвердить сохранение. Повторите «Сохранить» с тем же составом.',
      'Save could not be confirmed. Retry Save with the same selection.',
    ],
  ],
  [
    'auth',
    [
      'Откройте редактор заново кнопкой в Telegram.',
      'Reopen the editor using its Telegram button.',
    ],
  ],
  ['names', ['Укажите имя и фамилию.', 'Enter first and last names.']],
  [
    'unavailable',
    [
      'Заказ недоступен. Откройте его кнопкой в Telegram.',
      'Order unavailable. Open it using its Telegram button.',
    ],
  ],
  [
    'response',
    [
      'Не удалось получить ответ. Попробуйте ещё раз.',
      'Could not get a response. Please try again.',
    ],
  ],
  ['quantity', ['Количество', 'Quantity']],
  ['friday', ['Пятница', 'Friday']],
  ['saturday', ['Суббота', 'Saturday']],
  ['sunday', ['Воскресенье', 'Sunday']],
  ['lunch', ['Обед', 'Lunch']],
  ['dinner', ['Ужин', 'Dinner']],
  ['preparty', ['Препати', 'Preparty']],
  ['shuttle', ['Трансфер', 'Transfer']],
  ['excursion_minsk', ['Экскурсия по Минску', 'Minsk tour']],
  [
    'excursion_grodno_overview',
    ['Обзорная экскурсия по Гродно', 'Grodno city tour'],
  ],
  ['excursion_grodno_gorodnitsa', ['Городница', 'Gorodnitsa tour']],
]);
const calendarOrder = new Map([
  ['friday', 0],
  ['saturday', 1],
  ['sunday', 2],
  ['lunch', 0],
  ['dinner', 1],
]);
function calendarEntries(value) {
  return Object.entries(value || {}).toSorted(
    ([left], [right]) =>
      (calendarOrder.get(left) ?? 99) - (calendarOrder.get(right) ?? 99) ||
      left.localeCompare(right),
  );
}
function text(key) {
  return labels.get(key)?.at($('#language').value === 'ru' ? 0 : 1) || key;
}
function localized(definition, ru, en) {
  const fields = new Map(Object.entries(definition || {}));
  const value =
    fields.get($('#language').value === 'ru' ? ru : en) || fields.get(en) || '';
  requireResponse(typeof value === 'string');
  return value;
}
function element(tag, content = '') {
  requireResponse(typeof content === 'string');
  const node = document.createElement(tag);
  node.textContent = content;
  return node;
}
function amount(value) {
  return Number(value).toFixed(2) + ' BYN';
}
function status(value) {
  $('#status').textContent = value;
}
function controls() {
  const isEditable = Boolean(state.data?.editable) && !state.isBusy;
  $('#fields').disabled = !isEditable;
  $('#quote').disabled = !isEditable;
  $('#save').disabled = !isEditable || !state.quoteInput;
  $('#reload').disabled = state.isBusy;
  $('#language').disabled = state.isBusy;
}
async function request(path, body) {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: {
      ...(initData && { Authorization: 'tma ' + initData }),
      'Content-Type': 'application/json',
    },
    ...(body !== undefined && { body: JSON.stringify(body) }),
  });
  let value;
  try {
    value = await response.json();
  } catch {
    const error = new Error('invalid_response');
    if (!response.ok) error.status = response.status;
    throw error;
  }
  if (!response.ok) {
    const error = new Error(value?.code || String(response.status));
    error.status = response.status;
    throw error;
  }
  return value;
}
function isRecord(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
function requireResponse(valid) {
  if (!valid) throw new Error('invalid_response');
}
// Validate consumed response fields before publishing data or clearing a retry.
function validateChoice(choice) {
  requireResponse(isRecord(choice) && Number.isFinite(choice.total));
  const names = new Map(Object.entries(choice));
  for (const key of [
    'customer_first_name',
    'customer_last_name',
    'customer_patronymus',
  ])
    requireResponse(
      names.get(key) === undefined || typeof names.get(key) === 'string',
    );
  requireResponse(isRecord(choice.extras) && isRecord(choice.days));
  for (const day of Object.values(choice.days)) {
    requireResponse(isRecord(day) && isRecord(day.mealtimes));
    for (const meal of Object.values(day.mealtimes)) {
      requireResponse(
        isRecord(meal) &&
          Number.isFinite(meal.total) &&
          Array.isArray(meal.dishes),
      );
      requireResponse(
        isRecord(meal.service) &&
          Number.isFinite(meal.service.total) &&
          Array.isArray(meal.service.items),
      );
      for (const item of [...meal.dishes, ...meal.service.items])
        requireResponse(
          isRecord(item) &&
            typeof item.name === 'string' &&
            Number.isSafeInteger(item.count) &&
            Number.isFinite(item.total),
        );
    }
  }
}
function validateOrder(order) {
  requireResponse(
    isRecord(order) &&
      Number.isSafeInteger(order.version) &&
      order.version >= 0,
  );
  validateChoice(order.choice);
}
function validateView(value) {
  requireResponse(
    isRecord(value) &&
      typeof value.editable === 'boolean' &&
      isRecord(value.event),
  );
  validateOrder(value.order);
  const { menu, extras } = value.event;
  requireResponse(isRecord(extras) && isRecord(menu));
  requireResponse(
    isRecord(menu.dishes) &&
      isRecord(menu.service_items) &&
      isRecord(menu.choices),
  );
  for (const definition of [
    ...Object.values(extras),
    ...Object.values(menu.dishes),
    ...Object.values(menu.service_items),
  ])
    requireResponse(isRecord(definition) && Number.isFinite(definition.price));
  for (const meals of Object.values(menu.choices)) {
    requireResponse(isRecord(meals));
    for (const groups of Object.values(meals)) {
      requireResponse(isRecord(groups));
      for (const names of Object.values(groups))
        requireResponse(
          Array.isArray(names) &&
            names.every(
              (name) =>
                typeof name === 'string' && Object.hasOwn(menu.dishes, name),
            ),
        );
    }
  }
}

function errorText(error) {
  if (error.status === 401) return text('auth');
  if ([403, 404].includes(error.status) || error.message === 'order_missing')
    return text('unavailable');
  if (error.status >= 500 || error.message === 'invalid_response')
    return text('response');
  if (error.message === 'stale_version') return text('conflict');
  return error.status ? error.message : text('response');
}
function customerFields(choice) {
  const fields = [
    ['customer_first_name', 'first', choice.customer_first_name || '', true],
    ['customer_last_name', 'last', choice.customer_last_name || '', true],
    ['customer_patronymus', 'middle', choice.customer_patronymus || '', false],
  ];
  for (const [name, label, value, required] of fields) {
    const wrapper = element('label', text(label));
    const input = element('input');
    input.name = name;
    input.value = value;
    input.required = required;
    input.maxLength = 256;
    wrapper.append(input);
    state.names.set(name, input);
    $('#customer').append(wrapper);
  }
}
function extraFields(choice) {
  $('#extras').append(element('h2', text('extras')));
  const selected = new Map(Object.entries(choice.extras || {}));
  for (const [key, definition] of Object.entries(state.data.event.extras)) {
    if (definition.legacy && !selected.has(key)) continue;
    const label = element('label');
    label.className = 'extra';
    const input = element('input');
    input.type = 'checkbox';
    input.checked = selected.has(key);
    input.name = key;
    label.append(
      input,
      document.createTextNode(
        ' ' + text(key) + ' · ' + amount(definition.price),
      ),
    );
    state.extras.set(key, input);
    $('#extras').append(label);
  }
}
function dishField(name, definition, quantity) {
  const row = element('div');
  row.className = 'dish';
  const description = element('div');
  const title = localized(definition, 'name_ru', 'name_en') || name;
  description.append(
    element('strong', title + ' · ' + amount(definition.price)),
  );
  description.append(
    element('p', localized(definition, 'ingredients_ru', 'ingredients_en')),
  );
  description.append(element('p', definition.output || ''));
  const label = element('label', text('quantity'));
  const input = element('input');
  input.type = 'number';
  input.min = '0';
  input.max = '10000';
  input.step = '1';
  input.value = String(quantity);
  input.setAttribute('aria-label', title);
  label.append(input);
  row.append(description, label);
  return { row, input };
}
function mealFields(choice) {
  const menu = state.data.event.menu;
  const dishes = new Map(Object.entries(menu.dishes));
  const categories = new Map(Object.entries(menu.category_labels || {}));
  const chosenDays = new Map(Object.entries(choice.days || {}));
  for (const [day, meals] of calendarEntries(menu.choices)) {
    const selectedMeals = new Map(
      Object.entries(chosenDays.get(day)?.mealtimes || {}),
    );
    const dayInputs = new Map();
    for (const [meal, groups] of calendarEntries(meals)) {
      const section = element('details');
      section.append(element('summary', text(day) + ' · ' + text(meal)));
      const selected = new Map();
      const selectedDishes = selectedMeals.get(meal)?.dishes || [];
      for (const item of selectedDishes) {
        selected.set(item.name, (selected.get(item.name) || 0) + item.count);
      }
      const inputs = new Map();
      for (const [category, names] of Object.entries(groups)) {
        const categoryName =
          localized(categories.get(category), 'ru', 'en') || category;
        section.append(element('h3', categoryName));
        for (const name of names) {
          const field = dishField(
            name,
            dishes.get(name),
            selected.get(name) || 0,
          );
          section.append(field.row);
          inputs.set(name, field.input);
        }
      }
      dayInputs.set(meal, inputs);
      $('#meals').append(section);
    }
    state.meals.set(day, dayInputs);
  }
}
function choiceInput() {
  const days = [];
  for (const [day, meals] of state.meals) {
    const selectedMeals = [];
    for (const [meal, inputs] of meals) {
      const dishes = [];
      for (const [name, input] of inputs) {
        const count = Number(input.value);
        if (count > 0) dishes.push({ name, count });
      }
      if (dishes.length > 0) selectedMeals.push([meal, { dishes }]);
    }
    if (selectedMeals.length > 0)
      days.push([day, { mealtimes: Object.fromEntries(selectedMeals) }]);
  }
  return {
    ...Object.fromEntries(
      [...state.names].map(([name, input]) => [name, input.value.trim()]),
    ),
    days: Object.fromEntries(days),
    extras: Object.fromEntries(
      [...state.extras]
        .filter(([, input]) => input.checked)
        .map(([key]) => [key, 0]),
    ),
  };
}
function renderSummary(choice) {
  const box = document.createDocumentFragment();
  box.append(element('h2', text('total') + ': ' + amount(choice.total)));
  const definitions = new Map(
    Object.entries(state.data.event.menu.service_items),
  );
  const days = calendarEntries(choice.days);
  for (const [day, value] of days) {
    const meals = calendarEntries(value.mealtimes);
    for (const [meal, selection] of meals) {
      const lines = element('ul');
      for (const item of selection.service.items) {
        const name =
          localized(definitions.get(item.name), 'name_ru', 'name_en') ||
          item.name;
        lines.append(
          element('li', `${name} × ${item.count}: ${amount(item.total)}`),
        );
      }
      box.append(
        element(
          'h3',
          text(day) + ' · ' + text(meal) + ': ' + amount(selection.total),
        ),
      );
      box.append(
        element('p', text('service') + ': ' + amount(selection.service.total)),
        lines,
      );
    }
  }
  $('#summary').replaceChildren(box);
}
function renderLabels() {
  for (const key of ['title', 'quote', 'save', 'reload'])
    $('#' + key).textContent = text(key);
  $('#customer-title').textContent = text('customer');
  document.documentElement.lang = $('#language').value;
}
function renderForm(choice) {
  renderLabels();
  for (const key of ['customer', 'extras', 'meals'])
    $('#' + key).replaceChildren();
  customerFields(choice);
  extraFields(choice);
  mealFields(choice);
  renderSummary(state.data.order.choice);
  controls();
}
// Keep the last complete form and its input maps if any rendering step fails.
function render(choice, data = state.data) {
  const previous = {
    data: state.data,
    names: state.names,
    extras: state.extras,
    meals: state.meals,
  };
  const sections = ['customer', 'extras', 'meals', 'summary'].map((key) => {
    const node = $('#' + key);
    return [node, [...node.childNodes]];
  });
  state.data = data;
  state.names = new Map();
  state.extras = new Map();
  state.meals = new Map();
  try {
    renderForm(choice);
  } catch {
    Object.assign(state, previous);
    for (const [node, children] of sections) node.replaceChildren(...children);
    throw new Error('invalid_response');
  }
}
async function load() {
  state.isBusy = true;
  renderLabels();
  controls();
  try {
    if (!orderId) throw new Error('order_missing');
    const data = await request(orderPath);
    validateView(data);
    render(data.order.choice, data);
    state.quoteInput = '';
    state.pending = undefined;
    status(state.data.editable ? '' : text('readonly'));
  } catch (error) {
    status(errorText(error));
  } finally {
    state.isBusy = false;
    controls();
  }
}
async function quote() {
  if (!$('#order-form').reportValidity()) return;
  const choice = choiceInput();
  if (!choice.customer_first_name || !choice.customer_last_name) {
    status(text('names'));
    return;
  }
  state.isBusy = true;
  controls();
  try {
    const quoted = await request(
      miniAppURL('api/quote?order_id=' + encodeURIComponent(orderId)),
      choice,
    );
    validateChoice(quoted);
    renderSummary(quoted);
    state.quoteInput = JSON.stringify(choice);
    status('');
  } catch (error) {
    state.quoteInput = '';
    status(errorText(error));
  } finally {
    state.isBusy = false;
    controls();
  }
}
async function save(event) {
  event.preventDefault();
  if (state.isBusy || !state.quoteInput || !$('#order-form').reportValidity())
    return;
  const choice = choiceInput();
  if (JSON.stringify(choice) !== state.quoteInput) return;
  state.pending ||= {
    version: state.data.order.version,
    key: crypto.randomUUID(),
    choice,
  };
  state.isBusy = true;
  controls();
  try {
    const order = await request(orderPath, state.pending);
    validateOrder(order);
    render(order.choice, { ...state.data, order });
    state.pending = undefined;
    state.quoteInput = '';
    status(text('saved'));
  } catch (error) {
    status(error.status ? errorText(error) : text('retry'));
    if (error.status && error.status < 500) state.quoteInput = '';
  } finally {
    state.isBusy = false;
    controls();
  }
}
$('#order-form').addEventListener('submit', save);
$('#quote').addEventListener('click', quote);
$('#reload').addEventListener('click', load);
$('#order-form').addEventListener('input', () => {
  state.quoteInput = '';
  state.pending = undefined;
  status(text('changed'));
  controls();
});
$('#language').addEventListener('change', () => {
  if (!state.data) {
    void load();
    return;
  }
  try {
    const draft = choiceInput();
    render(draft);
    state.quoteInput = '';
    status(text(state.data.editable ? 'changed' : 'readonly'));
  } catch (error) {
    status(errorText(error));
  }
  controls();
});
await browserSignIn(initData);
await load();
