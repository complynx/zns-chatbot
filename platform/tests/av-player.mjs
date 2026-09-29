import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

const [html, script, wav] = await Promise.all([
  readFile('internal/sandbox/index.html'),
  readFile('internal/sandbox/app.js'),
  readFile('testdata/media/orders-en.wav'),
]);
const browser = await chromium.launch({
  channel:
    process.env.BROWSER_CHANNEL ||
    (process.platform === 'win32' ? 'msedge' : undefined),
});
try {
  for (const touch of [false, true]) {
    const context = await browser.newContext({ hasTouch: touch });
    const page = await context.newPage();
    const messages = [
      {
        message_id: 1,
        from: { is_bot: false },
        text: '',
        reply_markup: { inline_keyboard: [] },
        voice: { file_id: 'voice', duration: 2 },
      },
    ];
    await page.route('https://sandbox.test/**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === '/')
        return route.fulfill({ contentType: 'text/html', body: html });
      if (path === '/app.js')
        return route.fulfill({ contentType: 'text/javascript', body: script });
      if (path === '/lab/files/voice')
        return route.fulfill({
          status: 206,
          contentType: 'audio/wav',
          headers: {
            'Accept-Ranges': 'bytes',
            'Content-Range': `bytes 0-${wav.length - 1}/${wav.length}`,
            'Content-Length': String(wav.length),
          },
          body: wav,
        });
      if (path === '/lab/input') {
        messages.push({
          message_id: 2,
          from: { is_bot: true },
          text: 'New bot reply',
          reply_markup: { inline_keyboard: [] },
        });
        return route.fulfill({ json: {} });
      }
      return route.fulfill({ json: { messages, history: [], edits: 0 } });
    });
    await page.goto('https://sandbox.test/');
    const player = await page.locator('audio').elementHandle();
    assert.ok(player);
    await player.evaluate((audio) => audio.load());
    await page.waitForFunction(
      () => document.querySelector('audio')?.readyState === 4,
    );
    await player.evaluate((audio) => {
      audio.currentTime = 0.4;
    });
    await page.waitForFunction(
      () => document.querySelector('audio')?.currentTime >= 0.39,
    );
    if (touch) await page.locator('#orders').tap();
    else await page.locator('#orders').click();
    await page.getByText('New bot reply', { exact: true }).waitFor();
    assert.equal(await player.evaluate((audio) => audio.isConnected), true);
    assert.ok((await player.evaluate((audio) => audio.currentTime)) >= 0.39);
    await context.close();
  }
  console.log('AV player retains position across chat updates: mouse + touch');
} finally {
  await browser.close();
}
