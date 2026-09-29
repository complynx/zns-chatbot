// The renderer can be checked without running a Telegram or database stand.
import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';

const source = await fs.readFile('internal/sandbox/app.js', 'utf8');
const browser = await chromium.launch({
  headless: true,
  ...(process.env.BROWSER_CHANNEL && { channel: process.env.BROWSER_CHANNEL }),
});
try {
  const page = await browser.newPage();
  await page.setContent('<div id="output"></div>');
  await page.addScriptTag({
    content: source.slice(0, source.indexOf('async function post(')),
  });
  for (const type of ['blockquote', 'pre']) {
    const result = await page.evaluate((type) => {
      const output = document.querySelector('#output');
      output.replaceChildren();
      globalThis.messageText(
        output,
        {
          text: 'A B C',
          entities: [
            { type: 'bold', offset: 2, length: 1 },
            { type, offset: 0, length: 5 },
          ],
        },
        {},
        'alice',
      );
      return {
        html: output.getHTML(),
        text: output.textContent,
        blocks: output.children.length,
      };
    }, type);
    assert.equal(result.blocks, 1);
    assert.equal(result.text, 'A B C');
    assert.equal(result.html, `<${type}>A <strong>B</strong> C</${type}>`);
  }
  console.log('Entity block continuity: passed');
} finally {
  await browser.close();
}
