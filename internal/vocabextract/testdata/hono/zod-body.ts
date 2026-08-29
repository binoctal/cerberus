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

// Rich shapes (the run35 empty-body family): refinements, enums, arrays,
// nested objects, optionals, defaults — the minimal legal body must include
// every REQUIRED field with a value that satisfies its refinements, and omit
// optional/defaulted ones.
const Rich = z.object({
	name: z.string().min(1),
	email: z.string().email(),
	role: z.enum(['admin', 'user']),
	age: z.number().int().min(0),
	bio: z.string().optional(),
	tags: z.array(z.string()),
	addr: z.object({ city: z.string() }),
	status: z.string().default('new'),
});
app.post('/api/rich', zValidator('json', Rich), (c) => c.json({}));

// Handler-side schema.parse pattern (open-agents' other dominant form: no
// zValidator middleware, the handler parses c.req.json() itself).
const Parsed = z.object({ title: z.string(), n: z.number() });
app.post('/api/parse', async (c) => {
	const body = await c.req.json();
	const v = Parsed.parse(body);
	return c.json({});
});

// Handler-side parse of an unextractable schema — must OMIT min_body.
const PickyParsed = z.object({ a: z.string().refine(() => true) });
app.post('/api/picky-parse', async (c) => {
	const body = await c.req.json();
	const v = PickyParsed.parse(body);
	return c.json({});
});
