import { Hono } from 'hono';
import { z } from 'zod';
import { zValidator } from '@hono/zod-validator';

const app = new Hono();

// Declared schema referenced by zValidator (open-agents' dominant form).
const CreateThing = z.object({
	name: z.string(),
	count: z.number(),
	active: z.boolean(),
});
app.post('/api/things', zValidator('json', CreateThing), (c) => c.json({}));

// Inline schema.
app.post('/api/inline', zValidator('json', z.object({ label: z.string() })), (c) => c.json({}));

// Unextractable: refine + nested object — must OMIT min_body, never guess.
const Picky = z.object({ a: z.string().refine(() => true), nested: z.object({ b: z.string() }) });
app.post('/api/picky', zValidator('json', Picky), (c) => c.json({}));

// No schema at all.
app.post('/api/bare', (c) => c.json({}));
export default app;
