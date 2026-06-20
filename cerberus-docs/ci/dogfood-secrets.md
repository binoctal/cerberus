# Dogfood Workflow - Required GitHub Secrets

The weekly dogfood workflow (`.github/workflows/dogfood.yml`) requires three GitHub repository secrets to run real LLM tests against cerberus fixtures.

## Required Secrets

### 1. ANTHROPIC_AUTH_TOKEN
- **Description**: Anthropic API authentication token for accessing Claude models
- **Format**: Bearer token (typically starts with `sk-ant-`)
- **Source**: Anthropic Console (https://console.anthropic.com/)
- **Usage**: Passed to cerberus as `ANTHROPIC_AUTH_TOKEN` environment variable
- **Notes**: 
  - This token is used by cerberus to authenticate with the Anthropic API
  - Must have access to the model specified in `ANTHROPIC_DEFAULT_SONNET_MODEL`
  - Should be a valid API key with sufficient quota for weekly test runs

### 2. ANTHROPIC_BASE_URL
- **Description**: Base URL for the Anthropic API endpoint
- **Format**: HTTPS URL (e.g., `https://api.anthropic.com`)
- **Source**: Anthropic documentation or custom deployment
- **Usage**: Passed to cerberus as `ANTHROPIC_BASE_URL` environment variable
- **Notes**:
  - For production Anthropic API: `https://api.anthropic.com`
  - For custom deployments or proxies: use the appropriate base URL
  - cerberus uses this to construct the full API endpoint

### 3. ANTHROPIC_DEFAULT_SONNET_MODEL
- **Description**: Model identifier for the default Sonnet model to use in tests
- **Format**: Model string (e.g., `claude-sonnet-4-20250514`)
- **Source**: Anthropic model documentation
- **Usage**: Passed to cerberus as `ANTHROPIC_DEFAULT_SONNET_MODEL` environment variable
- **Notes**:
  - This is the model that cerberus will use for all AI operations during tests
  - Should be a current Sonnet model version
  - Model must be available to the API key in `ANTHROPIC_AUTH_TOKEN`

## Setting Up Secrets

### Via GitHub Web UI:
1. Navigate to repository Settings → Secrets and variables → Actions
2. Click "New repository secret"
3. Add each of the three secrets with their corresponding values

### Via GitHub CLI:
```bash
gh secret set ANTHROPIC_AUTH_TOKEN
gh secret set ANTHROPIC_BASE_URL
gh secret set ANTHROPIC_DEFAULT_SONNET_MODEL
```

## Verification

After setting secrets, verify the workflow runs successfully:
1. Go to Actions tab in GitHub
2. Select "Dogfood - Real LLM Tests" workflow
3. Click "Run workflow" to manually trigger a test
4. Verify all three fixture jobs (go-lib, node-app, python-pkg) complete successfully

## Security Notes

- **Never commit secrets to the repository**
- **Rotate secrets regularly** (especially if compromised)
- **Use least-privilege API keys** (only grant necessary permissions)
- **Monitor API usage** to ensure tests aren't consuming excessive quota
- **Use separate API keys** for CI/CD and development environments

## Troubleshooting

### Common Issues:

1. **Authentication failures**: Verify `ANTHROPIC_AUTH_TOKEN` is valid and not expired
2. **Model not found**: Ensure `ANTHROPIC_DEFAULT_SONNET_MODEL` matches available models
3. **Network errors**: Check `ANTHROPIC_BASE_URL` is correct and accessible
4. **Rate limiting**: Ensure API key has sufficient quota for weekly test runs

### Debug Mode:

To enable additional logging, temporarily modify the workflow to add `-v -v` flags to the cerberus command:
```yaml
run: ./build/cerberus -p test/fixtures/${{ matrix.fixture }} -v -v
```

## Fixture Details

The workflow tests three fixtures weekly:
- **go-lib**: Go library with coverage tests
- **node-app**: Node.js application with Jest tests
- **python-pkg**: Python package with pytest tests

Each fixture runs independently with full cerberus analysis including real LLM calls.
