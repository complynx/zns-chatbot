import assert from 'node:assert/strict';
import { mkdir, readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

const stand = JSON.parse(
  await readFile('test-results/browser-auth-stand.json', 'utf8'),
);
const output = 'test-results/public-mount';
await mkdir('test-results/public-mount', { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
const publicBase = new URL(stand.browser);
const prefix = publicBase.pathname.replace(/\/$/, '');
const errors = [];
const paths = new Set();
function observe(page) {
  page.on('pageerror', (error) => {
    errors.push(error.message);
  });
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.origin !== publicBase.origin) return;
    paths.add(url.pathname);
    if (prefix && !url.pathname.startsWith(prefix + '/'))
      errors.push(url.pathname);
  });
}
async function activate(locator, touch) {
  if (touch) await locator.tap();
  else await locator.click();
}
try {
  const context = await browser.newContext();
  const page = await context.newPage();
  const chat = await context.newPage();
  observe(page);
  await chat.goto(stand.telegram);
  await chat.locator('#telegram-username').fill('alice');
  await page.goto(`${stand.browser}/miniapp/?order_id=${stand.order}`);
  await page.locator('#language').selectOption('en');
  await page.getByLabel('Telegram username').fill('alice');
  await page.getByRole('button', { name: 'Sign in through Telegram' }).click();
  await page
    .locator('#browser-auth')
    .getByText(/Approve the request/)
    .waitFor();
  await page.reload();
  await chat
    .getByRole('button', { name: 'Разрешить вход', exact: true })
    .last()
    .click();
  await page.locator('#browser-auth').waitFor({ state: 'detached' });
  const cookies = await context.cookies();
  const session = cookies.find(
    (cookie) => cookie.name === 'zns_browser_session',
  );
  assert.equal(session.path, prefix + '/miniapp');
  assert.equal(session.httpOnly, true);
  assert.equal(session.sameSite, 'Strict');
  const storageState = await context.storageState();
  for (const language of ['en', 'ru']) {
    for (const touch of [false, true]) {
      const sample = await browser.newContext({
        storageState,
        hasTouch: touch,
        isMobile: touch,
      });
      const view = await sample.newPage();
      observe(view);
      await view.goto(`${stand.browser}/miniapp/?order_id=${stand.order}`);
      await view.locator('#language').selectOption(language);
      await view.locator('[name="customer_first_name"]').fill('Prefix');
      await view.locator('[name="customer_last_name"]').fill('Check');
      const quoted = view.waitForResponse((response) =>
        response.url().includes('/api/quote?'),
      );
      await activate(view.locator('#quote'), touch);
      const quoteResponse = await quoted;
      assert.equal(quoteResponse.status(), 200);
      const saved = view.waitForResponse(
        (response) =>
          response.url().includes('/api/orders/') &&
          response.request().method() === 'POST',
      );
      await activate(view.locator('#save'), touch);
      const saveResponse = await saved;
      assert.equal(saveResponse.status(), 200);
      for (const route of ['/miniapp/massage', '/massage_timetable']) {
        await view.goto(`${stand.browser}${route}?lang=${language}`);
        await view
          .getByRole('button', {
            name: language === 'en' ? 'Sign out' : 'Выйти',
            exact: true,
          })
          .waitFor();
        await view
          .locator('#status')
          .getByText(
            language === 'en' ? /Only event specialists/ : /специалист/,
          )
          .waitFor();
      }
      await view.goto(
        `${stand.browser}/menu?pass_key=food-browser&lang=${language}`,
      );
      await view.locator('#menu').waitFor({ state: 'visible' });
      await activate(view.locator('details summary').first(), touch);
      await view.locator('img').first().waitFor({ state: 'visible' });
      await view.waitForFunction(
        () => document.querySelector('img').naturalWidth > 0,
      );
      await activate(
        view.locator('#days input[type="checkbox"]').first(),
        touch,
      );
      const foodSaved = view.waitForResponse(
        (response) =>
          response.url().endsWith('/api/food') &&
          response.request().method() === 'POST',
      );
      await activate(view.locator('#menu button[type="submit"]'), touch);
      const foodResponse = await foodSaved;
      assert.equal(foodResponse.status(), 200);
      await view.screenshot({
        path: `${output}/${language}-${touch ? 'touch' : 'mouse'}.png`,
        fullPage: true,
      });
      await sample.close();
    }
  }
  await page.goto(`${stand.browser}/massage_timetable?lang=ru`);
  await page.getByRole('button', { name: 'Выйти', exact: true }).click();
  await page.locator('#browser-auth').waitFor();
  const afterLogout = await context.cookies();
  assert.equal(
    afterLogout.some((cookie) => cookie.name === 'zns_browser_session'),
    false,
  );
  assert.deepEqual(errors, []);
  for (const path of [
    '/miniapp/auth/start',
    '/miniapp/auth/status',
    '/miniapp/auth/logout',
    '/miniapp/api/quote',
    '/miniapp/api/food/quote',
    '/miniapp/foodphotos/fish.jpg',
  ]) {
    assert.ok(paths.has(prefix + path), path);
  }
  console.log(
    JSON.stringify(
      {
        base: stand.browser,
        languages: ['en', 'ru'],
        inputs: ['mouse', 'touch'],
        paths: [...paths],
        errors,
      },
      undefined,
      2,
    ),
  );
} finally {
  await browser.close();
}
