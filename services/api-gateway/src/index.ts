import express, { NextFunction, Request, Response } from 'express';
import cors from 'cors';
import morgan from 'morgan';
import rateLimit from 'express-rate-limit';
import http from 'http';
import https from 'https';

const PORT = parseInt(process.env['GATEWAY_PORT'] ?? '3000', 10);
const PAYMENT_SERVICE_URL = process.env['PAYMENT_SERVICE_URL'] ?? 'http://localhost:8080';
const hopByHopHeaders = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
]);

export function createApp(paymentServiceUrl = PAYMENT_SERVICE_URL): express.Express {
  const app = express();
  const allowedOrigins = (process.env['ALLOWED_ORIGINS'] ?? 'http://localhost:5173')
    .split(',')
    .map((origin) => origin.trim())
    .filter(Boolean);

  app.use(cors({
    origin: allowedOrigins,
    methods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'],
    allowedHeaders: ['Content-Type', 'Authorization', 'Idempotency-Key', 'X-Request-ID'],
  }));
  app.use(morgan('combined'));
  app.use(rateLimit({
    windowMs: 60 * 1000,
    max: 200,
    standardHeaders: true,
    legacyHeaders: false,
    message: { error: 'Too many requests, please try again later.' },
  }));

  app.get('/health', (_req: Request, res: Response) => {
    res.json({ status: 'ok', service: 'api-gateway', timestamp: new Date().toISOString() });
  });

  app.use('/api', (req: Request, res: Response) => {
    let target: URL;
    try {
      target = new URL(req.originalUrl.slice('/api'.length) || '/', paymentServiceUrl);
    } catch (err) {
      console.error('[gateway] invalid payment service URL:', err);
      res.status(500).json({ error: 'Internal Server Error' });
      return;
    }
    const headers: http.OutgoingHttpHeaders = { ...req.headers, host: target.host };
    for (const header of hopByHopHeaders) delete headers[header];
    const transport = target.protocol === 'https:' ? https : http;
    const upstream = transport.request(target, { method: req.method, headers }, (upstreamResponse) => {
      res.statusCode = upstreamResponse.statusCode ?? 502;
      for (const [name, value] of Object.entries(upstreamResponse.headers)) {
        if (value !== undefined && !hopByHopHeaders.has(name.toLowerCase())) {
          res.setHeader(name, value);
        }
      }
      upstreamResponse.pipe(res);
    });
    upstream.on('error', (err: Error) => {
      console.error('[gateway] proxy error:', err.message);
      if (!res.headersSent) {
        res.status(502).json({
          error: 'BadGateway',
          message: 'Payment service is temporarily unavailable.',
        });
      } else {
        res.destroy(err);
      }
    });
    req.pipe(upstream);
    res.on('close', () => {
      if (!res.writableEnded) upstream.destroy();
    });
  });

  app.use((_req: Request, res: Response) => {
    res.status(404).json({ error: 'Not Found' });
  });

  app.use((err: Error, _req: Request, res: Response, _next: NextFunction) => {
    console.error('[gateway] unhandled error:', err);
    res.status(500).json({ error: 'Internal Server Error' });
  });
  return app;
}

const app = createApp();
if (require.main === module) {
  app.listen(PORT, () => {
    console.log(`[gateway] listening on :${PORT} -> proxying to ${PAYMENT_SERVICE_URL}`);
  });
}

export default app;
