# Failure Reason Classification System

## Overview

Cerberus now classifies test failures by root cause, distinguishing between genuine system bugs and LLM/environment issues. This prevents misleading reports where "12 failures" might imply serious system problems when most failures are actually due to LLM output quality issues.

## Failure Reasons

### `FailureReasonNone` (Passed)
- Test passed successfully
- Used for all non-failed verdicts

### `FailureReasonAssertionFailed` (System Bug)
- **Genuine functional test failure**
- Examples: assertion failures, unexpected behavior, logic errors
- **This is the only failure type that represents a real system bug requiring investigation**

### `FailureReasonLLMQuality` (Not a Bug)
- **LLM output quality issues**
- Examples: JSON parsing failures, malformed responses, incomplete outputs
- Root cause: LLM limitations, not system bugs
- Our JSON parsing improvements handle these gracefully

### `FailureReasonPolicyRejected` (Expected Behavior)
- **Security/policy rejections**
- Examples: sandbox denied, dangerous commands blocked
- This is EXPECTED behavior, not a bug
- Safety mechanisms working as designed

### `FailureReasonDependencyMissing` (Environment Issue)
- **Dependency/environment problems**
- Examples: build failures, missing libraries, configuration errors
- Setup/environment issue, not a system bug

### `FailureReasonTimeout` (Performance Issue)
- **Test timeout**
- Could be system slowness or timeout set too low
- Needs investigation to determine root cause

### `FailureReasonSystemError` (System Bug)
- **Unexpected system errors**
- Examples: crashes, panics, internal errors
- **This IS a system bug that needs attention**

## Usage

### When Creating Verdicts

```go
// In test execution or judgment code
var failureReason store.FailureReason

if isParseError(err) {
    failureReason = store.FailureReasonLLMQuality
} else if isPolicyRejection(err) {
    failureReason = store.FailureReasonPolicyRejected
} else if isDependencyIssue(err) {
    failureReason = store.FailureReasonDependencyMissing
} else if isSystemError(err) {
    failureReason = store.FailureReasonSystemError
} else if isAssertionFailure(err) {
    failureReason = store.FailureReasonAssertionFailed
} else {
    failureReason = store.FailureReasonNone // Pass
}

_, err := s.CreateVerdict(
    ctx, sessionID, traceID, 
    target, status, confidence, 
    source, reasoning, suggestions, 
    failureReason, // NEW parameter
)
```

### In Reports

Reports now show:

1. **Summary Table** with failure breakdown:
   ```markdown
   ### Failure Breakdown
   
   | Failure Type | Count | Is System Bug? |
   |---------------|-------|----------------|
   | **LLM Quality Issue** | 8 | ❌ No |
   | **Policy Rejected** | 2 | ❌ No |
   | **Functional Failure** | 1 | ✅ Yes |
   
   🎉 **Good News:** Only 1 failure is a system bug!
   ```

2. **Verdicts Table** with failure reason column:
   ```markdown
   | # | Target | Status | Confidence | Failure Reason | Source |
   |---|--------|--------|------------|----------------|--------|
   | 1 | GET /health | ✅ pass | 0.99 | — | judge |
   | 2 | POST /login | ❌ fail | 0.70 | LLM Quality Issue | agent |
   ```

## Helper Methods

```go
// Check if a failure reason indicates a system bug
reason.IsSystemBug() // true for AssertionFailed, SystemError

// Check if it's an environment issue
reason.IsEnvironmentIssue() // true for DependencyMissing, Timeout

// Check if it's expected behavior
reason.IsExpectedBehavior() // true for PolicyRejected

// Check if it's an LLM issue
reason.IsLLMIssue() // true for LLMQuality

// Get human-readable display name
reason.DisplayName() // "Functional Failure", "LLM Quality Issue", etc.
```

## Examples

### Example 1: ReAct Loop JSON Parsing Error
```go
// In agent ReAct loop
if isParseError(steerErr) && strings.Contains(steerErr, "unmarshal") {
    failureReason = store.FailureReasonLLMQuality
    log.Debug("JSON parse error - LLM quality issue", 
        zap.String("error", steerErr))
}
```

### Example 2: Policy Rejection
```go
// In executor
if policyError != nil {
    failureReason = store.FailureReasonPolicyRejected
    log.Info("Command rejected by security policy")
}
```

### Example 3: Real Functional Failure
```go
// In examiner judgment
if correctness < threshold && !isParseIssue {
    failureReason = store.FailureReasonAssertionFailed
    log.Warn("Functional test failure detected",
        zap.Float64("correctness", correctness))
}
```

## Migration Notes

- Database migration V006 adds `failure_reason` column to `verdicts` table
- All existing CreateVerdict calls have been updated with appropriate defaults
- Test verdicts use `FailureReasonNone` for pass, `FailureReasonAssertionFailed` for fail
- Backward compatible: old records default to empty string (treated as `FailureReasonNone`)

## Benefits

1. **Prevents Misleading Reports**: Users won't panic over "12 failures" when 8 are LLM issues
2. **Focuses Attention**: Developers can immediately see which failures need investigation
3. **Better Debugging**: Clear categorization helps identify patterns (e.g., many LLM quality issues might prompt prompt engineering)
4. **Improves Confidence**: When reports show "0 system bugs", team can deploy with confidence

## Future Enhancements

- Add failure reason trends across sessions (e.g., "LLM quality issues increased 20% this week")
- Suggest prompt improvements when LLM quality issues exceed threshold
- Auto-classify failures based on error message patterns
- Dashboard visualization of failure reason distribution
