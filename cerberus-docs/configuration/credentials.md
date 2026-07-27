# Credentials

## Priority Order

1. **Environment variables** — `CERBERUS_ACTOR_<NAME>_EMAIL` / `_PASSWORD`
2. **Credentials file** — `.cerberus/credentials.yaml`
3. **Project config** — inline in `project.yaml`

## Environment Variables

```bash
export CERBERUS_ACTOR_ADMIN_EMAIL="admin@example.com"
export CERBERUS_ACTOR_ADMIN_PASSWORD="secret"
export CERBERUS_ACTOR_ADMIN_TOKEN="api-key-or-dev-token"
```

Actor name is uppercased with hyphens replaced by underscores.

## Credentials File

`.cerberus/credentials.yaml`:

```yaml
actors:
  admin:
    email: admin@example.com
    password: changeme
    token: api-key-or-dev-token
```

!!! warning
    Add `.cerberus/credentials.yaml` to `.gitignore`. `cerberus init` does this automatically.

## Interpolation

In `project.yaml`, use `${VAR}` syntax:

```yaml
actors:
  - name: admin
    credentials:
      email: "${ADMIN_EMAIL}"
      password: "${ADMIN_PASS}"
```

If the env var is not set, the literal `${...}` is preserved.
