// Cerberus vocabulary extractor. Anchors on broadcastToWeb / sendToBridge
// call sites, walks up to the enclosing CaseClause and role-guard `if`,
// and emits one edge per type in the switch fall-through chain.
import { Project, SyntaxKind } from 'ts-morph';

const file = process.argv[2];
if (!file) { console.error('usage: node extractor.mjs <source.ts>'); process.exit(2); }
const project = new Project();
const sf = project.addSourceFileAtPath(file);

const lit = (n) => n?.getText().replace(/^['"`]|['"`]$/g, '');

// Collect a CaseClause's fall-through chain: preceding empty-body cases.
function fallThroughTypes(cc) {
  const block = cc.getParent();
  if (block.getKind() !== SyntaxKind.CaseBlock) return [lit(cc.getExpression())];
  const clauses = block.getClauses();
  const idx = clauses.indexOf(cc);
  const types = [lit(cc.getExpression())];
  for (let i = idx - 1; i >= 0; i--) {
    const c = clauses[i];
    if (c.getKind() !== SyntaxKind.CaseClause) break;
    if (c.getStatements().length > 0) break; // non-empty body stops the chain
    const e = c.getExpression();
    if (e) types.unshift(lit(e));
  }
  return types;
}

// Nearest `if (meta.type === 'web'|'bridge')` enclosing node.
function roleGuard(node) {
  for (let n = node; n; n = n.getParent()) {
    if (n.getKind() === SyntaxKind.IfStatement) {
      const cond = n.getExpression().getText();
      const m = cond.match(/meta\.type\s*===?\s*['"](web|bridge)['"]/);
      if (m) return { from_role: m[1], guard: cond };
    }
    if (n.getKind() === SyntaxKind.MethodDeclaration) break;
  }
  return { from_role: null, guard: null };
}

const edges = [];
const cls = sf.getClasses()[0];
for (const method of cls.getMethods()) {
  const mname = method.getName();
  for (const call of method.getDescendantsOfKind(SyntaxKind.CallExpression)) {
    const expr = call.getExpression();
    if (expr.getKind() !== SyntaxKind.PropertyAccessExpression) continue;
    const name = expr.getName();
    const isB2W = name === 'broadcastToWeb';
    const isW2B = name === 'sendToBridge';
    if (!isB2W && !isW2B) continue;

    const cc = call.getFirstAncestorByKind(SyntaxKind.CaseClause);
    const { from_role, guard } = roleGuard(call);
    const trigger = mname === 'handleMessage' ? 'message_handled'
                  : mname === 'fetch' ? 'fetch_branch'
                  : mname === 'webSocketClose' ? 'disconnect_bridge' : mname;
    const line = call.getStartLineNumber();
    const make = (type) => ({
      from_role: from_role ?? null,
      to_role: isB2W ? 'web' : 'bridge',
      type, trigger, guard,
      delivery: { mode: isB2W ? 'broadcast_web' : 'send_bridge_by_device' },
      source: { spans: [{ start: line, end: line }] },
    });
    if (cc) {
      for (const t of fallThroughTypes(cc)) edges.push(make(t));
    } else {
      const arg = call.getArguments().find(a => a.getKind() === SyntaxKind.ObjectLiteralExpression);
      const tp = arg?.getProperties().find(p => p.getKind() === SyntaxKind.PropertyAssignment && p.getName?.() === 'type');
      edges.push({ ...make(tp ? lit(tp.getInitializer()) : '(dynamic)'), best_effort: true });
    }
  }
}
console.log(JSON.stringify({ edges }));
