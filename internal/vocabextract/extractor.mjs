// Cerberus vocabulary extractor. Stub: validates the subprocess bridge by
// echoing one fixed edge regardless of input. Task 3 replaces the body with
// the emit-point algorithm.
import { Project, SyntaxKind } from 'ts-morph';

const file = process.argv[2];
if (!file) { console.error('usage: node extractor.mjs <source.ts>'); process.exit(2); }
const project = new Project();
project.addSourceFileAtPath(file); // parse to surface errors loudly
console.log(JSON.stringify({
  edges: [{ from_role: 'bridge', to_role: 'web', type: 'stub:type',
            trigger: 'message_handled', delivery: { mode: 'broadcast_web' },
            source: { spans: [{ start: 1, end: 1 }] } }],
}));
