const { after, before, test } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const { createApp } = require('../dist/index');

let server;
let upstream;
let baseURL;

before(async () => {
  upstream = http.createServer((req, res) => {
    const chunks = [];
    req.on('data', (chunk) => chunks.push(chunk));
    req.on('end', () => {
      res.setHeader('content-type', 'application/json');
      res.end(JSON.stringify({
        path: req.url,
        idempotencyKey: req.headers['idempotency-key'],
        body: Buffer.concat(chunks).toString(),
      }));
    });
  });
  await new Promise((resolve) => upstream.listen(0, '127.0.0.1', resolve));
  const upstreamURL = `http://127.0.0.1:${upstream.address().port}`;
  const app = createApp(upstreamURL);
  server = app.listen(0);
  await new Promise((resolve) => server.once('listening', resolve));
  baseURL = `http://127.0.0.1:${server.address().port}`;
});

after(async () => {
  await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  await new Promise((resolve, reject) => upstream.close((error) => error ? reject(error) : resolve()));
});

test('health endpoint reports gateway health without exposing upstream configuration', async () => {
  const response = await fetch(`${baseURL}/health`);
  const payload = await response.json();
  assert.equal(response.status, 200);
  assert.equal(payload.status, 'ok');
  assert.equal(payload.service, 'api-gateway');
  assert.equal(typeof payload.timestamp, 'string');
  assert.equal('upstream' in payload, false);
});

test('unknown routes return a JSON not-found response', async () => {
  const response = await fetch(`${baseURL}/unknown`);
  assert.equal(response.status, 404);
  assert.deepEqual(await response.json(), { error: 'Not Found' });
});

test('API proxy removes the prefix and streams the body and idempotency header', async () => {
  const response = await fetch(`${baseURL}/api/v1/payments?limit=10`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': 'test-key' },
    body: JSON.stringify({ amount: 100 }),
  });
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), {
    path: '/v1/payments?limit=10',
    idempotencyKey: 'test-key',
    body: '{"amount":100}',
  });
});
