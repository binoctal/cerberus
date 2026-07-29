package report

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Cerberus Report — {{.Session.ID}}</title>
<style>
  :root { --pass: #22c55e; --fail: #ef4444; --uncertain: #eab308; --skip: #9ca3af; --recovered: #0ea5e9; --bg: #f8fafc; --border: #e2e8f0; }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: var(--bg); color: #1e293b; padding: 2rem; max-width: 960px; margin: 0 auto; }
  h1 { font-size: 1.5rem; margin-bottom: 1rem; }
  h2 { font-size: 1.2rem; margin: 1.5rem 0 0.75rem; color: #475569; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 1rem; font-size: 0.875rem; }
  th, td { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid var(--border); }
  th { font-weight: 600; color: #64748b; background: #f1f5f9; }
  tr:hover { background: #f1f5f9; }
  code { background: #f1f5f9; padding: 0.15rem 0.35rem; border-radius: 3px; font-size: 0.8rem; }
  .badge { display: inline-block; padding: 0.15rem 0.5rem; border-radius: 9999px; font-size: 0.75rem; font-weight: 600; color: #fff; }
  .badge-pass { background: var(--pass); }
  .badge-fail { background: var(--fail); }
  .badge-uncertain { background: var(--uncertain); color: #1e293b; }
  .badge-skip { background: var(--skip); }
  .badge-recovered { background: var(--recovered); }
  .badge-running { background: #3b82f6; }
  .badge-aborted { background: #7c3aed; }
  .badge-completed { background: var(--pass); }
  .badge-written { background: var(--pass); }
  .badge-reverted { background: var(--fail); }
  .badge-skipped { background: var(--skip); }
  .badge-failed { background: var(--fail); }
  .badge-generated { background: #3b82f6; }
  .summary-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 1rem; margin-bottom: 1rem; }
  .summary-card { background: #fff; border: 1px solid var(--border); border-radius: 8px; padding: 1rem; text-align: center; }
  .summary-card .value { font-size: 1.5rem; font-weight: 700; }
  .summary-card .label { font-size: 0.75rem; color: #64748b; text-transform: uppercase; }
  .detail { background: #fff; border: 1px solid var(--border); border-radius: 8px; padding: 1rem; margin-bottom: 0.75rem; }
  .detail summary { cursor: pointer; font-weight: 600; color: #475569; }
  .detail p { margin-top: 0.5rem; font-size: 0.875rem; color: #64748b; line-height: 1.5; }
  footer { margin-top: 2rem; font-size: 0.75rem; color: #94a3b8; text-align: center; }
</style>
</head>
<body>
<h1>🛡️ Cerberus Test Report</h1>

<h2>Session</h2>
<table>
  <tr><th>ID</th><td><code>{{.Session.ID}}</code></td></tr>
  <tr><th>Goal</th><td>{{.Session.Goal}}</td></tr>
  <tr><th>Status</th><td><span class="badge badge-{{.Session.Status}}">{{.Session.Status}}</span></td></tr>
  {{if .Session.ProjectName}}<tr><th>Project</th><td>{{.Session.ProjectName}}</td></tr>{{end}}
  <tr><th>Started</th><td>{{.Session.StartedAt}}</td></tr>
  {{if .Session.FinishedAt}}<tr><th>Finished</th><td>{{.Session.FinishedAt}}</td></tr>{{end}}
</table>

{{if .Summary}}{{if .Summary.TotalCases}}
<h2>Summary</h2>
<div class="summary-grid">
  <div class="summary-card"><div class="value" style="color:var(--pass)">{{.Summary.Passed}}</div><div class="label">Passed</div></div>
  <div class="summary-card"><div class="value" style="color:var(--fail)">{{.Summary.Failed}}</div><div class="label">Failed</div></div>
  <div class="summary-card"><div class="value" style="color:var(--skip)">{{.Summary.Skipped}}</div><div class="label">Skipped</div></div>
  <div class="summary-card"><div class="value" style="color:var(--uncertain)">{{.Summary.Uncertain}}</div><div class="label">Uncertain</div></div>
  <div class="summary-card"><div class="value" style="color:var(--recovered)">{{.Summary.Recovered}}</div><div class="label">Recovered</div></div>
  <div class="summary-card"><div class="value">{{.Summary.TotalCases}}</div><div class="label">Total</div></div>
  {{if .Summary.Duration}}<div class="summary-card"><div class="value">{{.Summary.Duration}}</div><div class="label">Duration</div></div>{{end}}
</div>
{{end}}{{end}}

{{if .Summary}}{{if .Summary.Failed}}
<h3>Failure Breakdown</h3>
<table>
  <tr><th>Failure Type</th><th>Count</th><th>Is System Bug?</th></tr>
  {{range indexFailureReasons .}}
  <tr>
    <td><strong>{{.Reason.DisplayName}}</strong></td>
    <td>{{.Count}}</td>
    <td>{{if .Reason.IsSystemBug}}✅ Yes{{else}}❌ No{{end}}</td>
  </tr>
  {{end}}
</table>
{{end}}{{end}}

{{if .Verdicts}}
<h2>Verdicts</h2>
<table>
  <tr><th>#</th><th>Target</th><th>Status</th><th>Confidence</th><th>Failure Reason</th><th>Source</th></tr>
  {{range $i, $v := .Verdicts}}
  <tr>
    <td>{{add $i 1}}</td>
    <td><code>{{$v.Target}}</code></td>
    <td><span class="badge badge-{{if $v.Recovered}}recovered{{else}}{{$v.Status}}{{end}}">{{$v.Status}}</span></td>
    <td>{{printf "%.2f" $v.Confidence}}</td>
    <td>{{if or (eq $v.Status "fail") (eq $v.Status "failed")}}{{if $v.FailureReason}}{{$v.FailureReason.DisplayName}}{{else}}—{{end}}{{else}}—{{end}}</td>
    <td>{{$v.Source}}</td>
  </tr>
  {{end}}
</table>

{{range .Verdicts}}{{if .Reasoning}}
<details class="detail">
  <summary><code>{{.Target}}</code> — {{.Status}}</summary>
  <p>{{.Reasoning}}</p>
</details>
{{end}}{{end}}

{{if .Evidence}}
<h2>Evidence</h2>
{{range .Verdicts}}{{if index $.Evidence .TraceID}}
<details class="detail">
  <summary><code>{{.Target}}</code> — evidence</summary>
  {{range $i, $ev := (index $.Evidence .TraceID)}}
  <div class="evidence-item">
    <strong>[{{$ev.Type}}]</strong>
    <pre>{{truncate $ev.Content 500}}</pre>
  </div>
  {{end}}
</details>
{{end}}{{end}}
{{end}}
{{end}}

{{if .Traces}}
<h2>Execution Timeline</h2>
<table>
  <tr><th>#</th><th>Category</th><th>Target</th><th>Status</th><th>Started</th></tr>
  {{range $i, $t := .Traces}}
  <tr>
    <td>{{add $i 1}}</td>
    <td>{{$t.Category}}</td>
    <td><code>{{$t.Target}}</code></td>
    <td><span class="badge badge-{{$t.Status}}">{{$t.Status}}</span></td>
    <td>{{$t.StartedAt}}</td>
  </tr>
  {{end}}
</table>
{{end}}

{{if .AutoTest}}{{if .AutoTest.Items}}
<h2>AutoTest</h2>
<div class="summary-grid">
  <div class="summary-card"><div class="value">{{printf "%.1f" .AutoTest.BeforeCoveragePct}}% → {{printf "%.1f" .AutoTest.AfterCoveragePct}}%</div><div class="label">Coverage</div></div>
  <div class="summary-card"><div class="value">{{countStatus .AutoTest.Items "written"}}</div><div class="label">Written</div></div>
  <div class="summary-card"><div class="value">{{countStatus .AutoTest.Items "reverted"}}</div><div class="label">Reverted</div></div>
  <div class="summary-card"><div class="value">{{countStatus .AutoTest.Items "skipped"}}</div><div class="label">Skipped</div></div>
  <div class="summary-card"><div class="value">{{countStatus .AutoTest.Items "failed"}}</div><div class="label">Failed</div></div>
</div>
<table>
  <tr><th>#</th><th>Test File</th><th>Target</th><th>Reason</th><th>Status</th></tr>
  {{range $i, $item := .AutoTest.Items}}
  <tr>
    <td>{{add $i 1}}</td>
    <td><code>{{$item.TestPath}}</code></td>
    <td>{{$item.TargetFunc}} <code>{{baseName $item.TargetFile}}</code></td>
    <td>{{$item.Reason}}</td>
    <td><span class="badge badge-{{$item.Status}}">{{$item.Status}}</span></td>
  </tr>
  {{end}}
</table>
{{end}}{{end}}

<footer>Generated by <strong>Cerberus</strong></footer>
</body>
</html>`
