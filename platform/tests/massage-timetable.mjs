import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

const browser = await chromium.launch({
  channel: process.env.BROWSER_CHANNEL || 'msedge',
  headless: true,
});
const base = process.env.TIMETABLE_URL;
assert.ok(base);
const output = 'test-results/massage-timetable';
await mkdir(output, { recursive: true });

function address(language, identity, path = '/miniapp/massage') {
  return `${base}${path}?event=sandbox-festival&lang=${language}#tgWebAppData=${encodeURIComponent(identity)}`;
}

try {
  for (const language of ['en', 'ru']) {
    for (const touch of [false, true]) {
      const context = await browser.newContext({
        viewport: touch
          ? { width: 390, height: 844 }
          : { width: 1280, height: 900 },
        hasTouch: touch,
        isMobile: touch,
      });
      const page = await context.newPage();
      const errors = [];
      page.on('pageerror', (error) => {
        errors.push(error.message);
      });
      await page.goto(process.env.SANDBOX_URL);
      await page.locator('#user').selectOption('202');
      const launch = page.getByRole('button', {
        name: 'Открыть расписание',
        exact: true,
      });
      if (touch) await launch.tap();
      else await launch.click();
      await page.frameLocator('#miniapp-frame').locator('.booking').waitFor();
      await page.goto(address(language, process.env.TIMETABLE_STAFF));
      await page.locator('.booking').waitFor();
      assert.equal(await page.locator('html').getAttribute('lang'), language);
      assert.equal(await page.locator('.booking').count(), 1);
      assert.match(await page.locator('.booking').textContent(), /Алиса/);
      assert.match(await page.locator('.booking').textContent(), /43 BYN/);
      assert.equal(await page.locator('.column:not(.axis)').count(), 2);
      const press = async (selector) => {
        if (touch) await page.locator(selector).tap();
        else await page.locator(selector).click();
      };
      await press('#mine');
      assert.equal(await page.locator('.column:not(.axis)').count(), 1);
      await press('#mine');
      assert.equal(await page.locator('.column:not(.axis)').count(), 2);
      const height = await page
        .locator('.timeline')
        .first()
        .evaluate((node) => node.offsetHeight);
      await press('#zoom-in');
      assert.ok(
        (await page
          .locator('.timeline')
          .first()
          .evaluate((node) => node.offsetHeight)) > height,
      );
      await press('#zoom-out');
      assert.equal(
        await page
          .locator('.timeline')
          .first()
          .evaluate((node) => node.offsetHeight),
        height,
      );
      await page.locator('#mine').focus();
      await page.keyboard.press('Space');
      assert.equal(await page.locator('.column:not(.axis)').count(), 1);
      await page.keyboard.press('Space');
      await page
        .locator('#language')
        .selectOption(language === 'en' ? 'ru' : 'en');
      assert.equal(
        await page.locator('html').getAttribute('lang'),
        language === 'en' ? 'ru' : 'en',
      );
      await page.locator('#language').selectOption(language);
      const beforeRefresh = await page
        .locator('#timetable')
        .evaluate((node) => {
          node.scrollTop = 1600;
          node.scrollLeft = 120;
          return { top: node.scrollTop, left: node.scrollLeft };
        });
      await press('#refresh');
      await page.locator('.booking').waitFor();
      assert.deepEqual(
        await page.locator('#timetable').evaluate((node) => ({
          top: node.scrollTop,
          left: node.scrollLeft,
        })),
        beforeRefresh,
      );
      await page.screenshot({
        path: `${output}/${language}-${touch ? 'touch' : 'mouse'}.png`,
        fullPage: true,
      });
      assert.deepEqual(errors, []);
      await page.goto('about:blank');
      await page.goto(address(language, process.env.TIMETABLE_CLIENT));
      await page.waitForFunction(
        () => !document.querySelector('#refresh').disabled,
      );
      assert.equal(await page.locator('.booking').count(), 0);
      assert.match(
        await page.locator('#status').textContent(),
        language === 'ru' ? /специалистам/ : /specialists/,
      );
      await page.goto('about:blank');
      await page.goto(address(language, ''));
      assert.equal(await page.locator('.booking').count(), 0);
      assert.match(await page.locator('#status').textContent(), /Telegram/);
      await page.goto(
        address(language, process.env.TIMETABLE_STAFF, '/massage_timetable'),
      );
      await page.locator('.booking').waitFor();
      await context.close();
    }
  }
  console.log(
    'Timetable EN/RU mouse, keyboard, emulated touch, refresh and denied/unsigned flows passed.',
  );
} finally {
  await browser.close();
}
