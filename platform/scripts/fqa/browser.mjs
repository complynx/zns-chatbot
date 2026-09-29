import { chromium } from 'playwright';
import { userID } from './client.mjs';
import { terminateOwnedTree } from './stand.mjs';

async function bounded(action, terminate, timeout = 10_000) {
  let timer;
  let termination;
  let failure;
  let result;
  const expired = new Error('Browser operation deadline exceeded');
  try {
    result = await Promise.race([
      action(),
      new Promise((_resolve, reject) => {
        timer = setTimeout(() => {
          termination = Promise.allSettled([terminate()]);
          void (async () => {
            await termination;
            reject(expired);
          })();
        }, timeout);
      }),
    ]);
  } catch (error) {
    failure = error;
  }
  clearTimeout(timer);
  if (termination) {
    const [joined] = await termination;
    if (joined.status === 'rejected')
      throw new AggregateError(
        [expired, joined.reason, ...(failure ? [failure] : [])],
        'Browser termination failed',
      );
    failure ||= expired;
  }
  if (failure) throw failure;
  return result;
}

// Upstream launchServer can stall before returning a process handle. Callers requiring
// a hard startup bound must own this entire operation in a child process (fqa-owned).
export async function openTelegram(
  run,
  {
    touch = false,
    subject = 'alice',
    channel = process.env.BROWSER_CHANNEL,
    headless = true,
    signal,
    label = touch ? 'touch' : 'mouse',
  } = {},
) {
  signal?.throwIfAborted();
  let browser;
  let server;
  let context;
  let page;
  let closing;
  let isTracing = false;
  const errors = [];
  let termination;
  let hasTerminated = false;
  const setupDeadline = Date.now() + 30_000;
  const remainingSetup = () =>
    Math.max(1, Math.min(10_000, setupDeadline - Date.now()));
  const terminate = async () => {
    if (!server) return;
    termination ||= terminateOwnedTree(server.process());
    await termination;
    hasTerminated = true;
  };
  const setup = (action) => bounded(action, terminate, remainingSetup());
  const close = (artifactLabel = label) => {
    closing ||= (async () => {
      const failures = [];
      const attempts = [
        async () => {
          if (page)
            await page.screenshot({
              path: run.artifact(artifactLabel + '.png'),
              fullPage: true,
              timeout: 5000,
            });
        },
        async () => {
          if (isTracing)
            await bounded(
              () =>
                context.tracing.stop({
                  path: run.artifact(artifactLabel + '-trace.zip'),
                }),
              terminate,
            );
        },
        terminate,
        () =>
          run.save(artifactLabel + '-process.json', {
            pid: server?.process().pid,
            joined: hasTerminated,
          }),
        () => run.save(artifactLabel + '-browser-errors.json', errors),
      ];
      for (const attempt of attempts) {
        try {
          await attempt();
        } catch (error) {
          failures.push(error);
        }
      }
      signal?.removeEventListener('abort', interrupt);
      if (failures.length > 0)
        throw new AggregateError(failures, 'Browser evidence/cleanup failed');
    })();
    return closing;
  };
  const interrupt = () => {
    if (server) void close().catch(() => {});
  };
  signal?.addEventListener('abort', interrupt, { once: true });
  try {
    server = await chromium.launchServer({
      headless,
      host: '127.0.0.1',
      handleSIGINT: false,
      handleSIGTERM: false,
      handleSIGHUP: false,
      timeout: 15_000,
      ...(channel && { channel }),
    });
    signal?.throwIfAborted();
    browser = await setup(() =>
      chromium.connect(server.wsEndpoint(), { timeout: remainingSetup() }),
    );
    signal?.throwIfAborted();
    context = await setup(() =>
      browser.newContext({
        viewport: touch
          ? { width: 390, height: 844 }
          : { width: 1440, height: 1000 },
        hasTouch: touch,
        isMobile: touch,
      }),
    );
    context.setDefaultTimeout(15_000);
    context.setDefaultNavigationTimeout(15_000);
    signal?.throwIfAborted();
    await setup(() =>
      context.tracing.start({ screenshots: true, snapshots: true }),
    );
    isTracing = true;
    page = await setup(() => context.newPage());
    page.on('pageerror', (error) => {
      errors.push(String(error));
    });
    signal?.throwIfAborted();
    await page.goto(run.client.ui, { timeout: remainingSetup() });
    await page
      .locator('#user')
      .selectOption(String(userID(subject)), { timeout: remainingSetup() });
    signal?.throwIfAborted();
    const click = (locator) => {
      signal?.throwIfAborted();
      return touch ? locator.tap() : locator.click();
    };
    const driver = {
      page,
      click,
      // Observe the real browser input response; completion tracks update ID, not message count.
      async act(action) {
        const [response] = await Promise.all([
          page.waitForResponse(
            (r) =>
              r.url() === run.client.ui + '/lab/input' &&
              r.request().method() === 'POST',
          ),
          action(),
        ]);
        if (!response.ok())
          throw new Error(`Telegram input rejected: ${response.status()}`);
        const update = await response.json();
        await run.client.processed(subject, update.update_id);
        return update;
      },
      async send(text) {
        await page.locator('#text').fill(text);
        return driver.act(() => click(page.locator('#chat button')));
      },
      card(messageID) {
        return page.locator(`.message[data-message-id="${Number(messageID)}"]`);
      },
      async download(link, name) {
        const [download] = await Promise.all([
          page.waitForEvent('download'),
          click(link),
        ]);
        const failure = await download.failure();
        if (failure) throw new Error(failure);
        await download.saveAs(run.artifact(name));
        return {
          filename: download.suggestedFilename(),
          path: run.artifact(name),
        };
      },
      close,
    };
    return driver;
  } catch (error) {
    const original = signal?.aborted ? signal.reason : error;
    try {
      await close();
    } catch (cleanupError) {
      throw new AggregateError(
        [original, error, cleanupError],
        'Browser setup and evidence failed',
        { cause: cleanupError },
      );
    }
    throw original;
  }
}
