import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const [english, russian, video, tooLong] = await Promise.all([
  readFile('testdata/media/orders-en.wav'),
  readFile('testdata/media/help-ru.wav'),
  readFile('testdata/media/later-amount.mp4'),
  readFile('testdata/media/too-long.mp4'),
]);
const broker = 'http://127.0.0.1:8097';
async function processMedia(query, body, operation = 'preprocess') {
  const response = await fetch(`${broker}/v1/${operation}?${query}`, {
    method: 'POST',
    headers: {
      Authorization: 'Bearer sandbox-only-media-broker',
      'Content-Type': 'application/octet-stream',
    },
    body,
    signal: AbortSignal.timeout(160_000),
  });
  assert.equal(response.status, 200);
  return response.json();
}
for (const [body, text] of [
  [english, 'Please show my orders.'],
  [russian, 'Какие услуги я могу забронировать?'],
]) {
  const result = await processMedia('kind=voice', body);
  assert.equal(result.status, 'ready');
  assert.deepEqual(result.transcript, { status: 'ok', text });
  assert.equal(result.frames?.length || 0, 0);
}
const initial = await processMedia('kind=video', video);
assert.equal(initial.status, 'ready');
assert.equal(initial.transcript.status, 'no_audio');
assert.equal(initial.frames.length, 1);
assert.deepEqual(initial.duration, { numerator: 20, denominator: 1 });
const refined = await processMedia(
  'kind=video&start_ms=10000&end_ms=19000&count=2',
  video,
  'storyboard',
);
assert.equal(refined.status, 'ready');
assert.equal(refined.transcript.status, 'not_run');
assert.equal(refined.frames.length, 2);
for (const frame of refined.frames) {
  const seconds = frame.timestamp.numerator / frame.timestamp.denominator;
  assert.ok(seconds >= 10 && seconds <= 19);
}
const rejected = await processMedia('kind=video', tooLong);
assert.equal(rejected.status, 'rejected');
assert.equal(rejected.reason, 'too_long');
assert.equal(rejected.frames?.length || 0, 0);
assert.equal(rejected.transcript.status, 'not_run');
console.log(
  'Real decoder + fixture ASR: EN/RU, sparse/range frames, duration refusal passed',
);
