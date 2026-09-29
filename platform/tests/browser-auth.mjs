import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

const stand = JSON.parse(
  await readFile('test-results/browser-auth-stand.json', 'utf8'),
);
const browser = await chromium.launch({ headless: true, channel: 'msedge' });
const context = await browser.newContext();
const page = await context.newPage();
const chat = await context.newPage();
const errors = [];
page.on('pageerror', (error) => {
  errors.push(error.message);
});
try {
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
  await page.locator('#language').selectOption('en');
  await page
    .locator('#browser-auth')
    .getByText(/Подтвердите запрос|Approve the request/)
    .waitFor();
  await chat
    .getByRole('button', { name: 'Разрешить вход', exact: true })
    .last()
    .click();
  await page.locator('#browser-auth').waitFor({ state: 'detached' });
  await page.getByRole('button', { name: 'Calculate', exact: true }).waitFor();
  await page.screenshot({
    path: 'test-results/browser-auth-editor.png',
    fullPage: true,
  });
  const cookies = await context.cookies();
  const session = cookies.find(
    (cookie) => cookie.name === 'zns_browser_session',
  );
  assert.equal(session.httpOnly, true);
  assert.equal(session.sameSite, 'Strict');
  assert.equal(session.path, '/miniapp');
  await page.goto(`${stand.browser}/massage_timetable?lang=en`);
  await page.getByRole('button', { name: 'Sign out', exact: true }).waitFor();
  await page
    .locator('#status')
    .getByText(/Only event specialists/)
    .waitFor();
  await page.screenshot({
    path: 'test-results/browser-auth-timetable-rights.png',
    fullPage: true,
  });
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await page.locator('#browser-auth').waitFor();
  await page.locator('#language').selectOption('ru');
  await page.getByLabel('Имя пользователя Telegram').fill('alice');
  await page
    .getByRole('button', { name: 'Войти через Telegram', exact: true })
    .click();
  await chat
    .getByRole('button', { name: 'Отклонить', exact: true })
    .last()
    .click();
  await page
    .locator('#browser-auth')
    .getByText('Вход отклонён. Можно повторить.')
    .waitFor();
  await page.screenshot({
    path: 'test-results/browser-auth-declined-ru.png',
    fullPage: true,
  });
  await page
    .getByRole('button', { name: 'Войти через Telegram', exact: true })
    .click();
  await page.getByRole('button', { name: 'Отменить', exact: true }).click();
  await page.locator('#browser-auth').getByText('Вход отменён.').waitFor();
  await chat.locator('#user').selectOption('202');
  await chat.locator('#telegram-username').fill('bob');
  const legacyResult = page.evaluate(async () => {
    const response = await fetch('/auth?username=bob', {
      credentials: 'include',
    });
    return { status: response.status, value: await response.json() };
  });
  await chat
    .getByRole('button', { name: 'Разрешить вход', exact: true })
    .last()
    .click();
  assert.deepEqual(await legacyResult, {
    status: 200,
    value: { result: 'authorized' },
  });
  assert.deepEqual(
    await page.evaluate(async () => {
      const response = await fetch('/auth?check=1', { credentials: 'include' });
      return response.json();
    }),
    { result: 'authorized' },
  );
  await page.getByLabel('Имя пользователя Telegram').fill('bob');
  await page
    .getByRole('button', { name: 'Войти через Telegram', exact: true })
    .click();
  await chat
    .getByRole('button', { name: 'Разрешить вход', exact: true })
    .last()
    .click();
  await page.locator('#browser-auth').waitFor({ state: 'detached' });
  await page.locator('#party').waitFor();
  await page.waitForFunction(() => !document.querySelector('#party').disabled);
  await page.screenshot({
    path: 'test-results/browser-auth-timetable-ru.png',
    fullPage: true,
  });
  assert.deepEqual(errors, []);
  console.log(
    'Browser consent via Telegram UI: editor EN, timetable rights, sign-out, RU decline/retry/cancel, legacy longpoll/check, specialist timetable RU passed.',
  );
} finally {
  await browser.close();
}
