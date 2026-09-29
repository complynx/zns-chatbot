import { SandboxClient } from './client.mjs';
import { Fixtures } from './fixture.mjs';
import { QARun } from './run.mjs';

const [command = 'help', argument, flag] = process.argv.slice(2);
const client = new SandboxClient();
switch (command) {
  case 'doctor': {
    const docker = new Fixtures().preflight().trim();
    const state = await client.state();
    const event = await client.json('api', '/v1/order-events/sandbox-festival');
    console.log(
      JSON.stringify(
        {
          docker,
          ui: client.ui,
          api: client.api,
          processedCursor: state.processed_cursor,
          event: event.id,
          deadline: event.deadline,
        },
        undefined,
        2,
      ),
    );
    break;
  }
  case 'state': {
    console.log(JSON.stringify(await client.state(argument), undefined, 2));
    break;
  }
  case 'orders': {
    console.log(JSON.stringify(await client.orders(argument), undefined, 2));
    break;
  }
  case 'cleanup': {
    if (!argument || (flag && flag !== '--cancel-proofs'))
      throw new Error('cleanup RUN_DIRECTORY [--cancel-proofs]');
    const run = await QARun.resume(argument);
    console.log(
      JSON.stringify(
        await run.cleanup({ cancelProofs: flag === '--cancel-proofs' }),
        undefined,
        2,
      ),
    );
    break;
  }
  case 'help': {
    console.log(
      'FQA: doctor | state [alice|bob|visitor] | orders [subject] | cleanup RUN_DIRECTORY [--cancel-proofs]\nLibrary and browser usage: scripts/fqa/README.md',
    );
    break;
  }
  default: {
    throw new Error('Unknown FQA command');
  }
}
