import { Hono } from 'hono';
import { nestedRoutes } from './nested';

const app = new Hono();
const rateLimiter = async (c: any, next: any) => { await next(); };

app.get('/', (c) => c.json({}));
app.get('/:id', (c) => c.json({}));
app.get('/:id/gated', rateLimiter, (c) => c.json({}));
app.on('GET', '/multi', (c) => c.json({}));
app.route('/nested', nestedRoutes);

export { app as thingRoutes };
