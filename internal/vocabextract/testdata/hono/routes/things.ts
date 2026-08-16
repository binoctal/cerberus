import { Hono } from 'hono';
import { nestedRoutes } from './nested';

const app = new Hono();

app.get('/', (c) => c.json({}));
app.get('/:id', (c) => c.json({}));
app.on('GET', '/multi', (c) => c.json({}));
app.route('/nested', nestedRoutes);

export { app as thingRoutes };
