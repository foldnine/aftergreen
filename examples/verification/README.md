# Verification contract example

A local demo of `aftergreen verify`. Nothing leaves your machine.

> `verify` is unreleased (planned for v0.2.0). Until then, build from `main`:
> `go install github.com/foldnine/aftergreen/cmd/aftergreen@main`, or run
> `go run ../../cmd/aftergreen` from this directory.

Files:

- `aftergreen.yaml`: two HTTP checks against a local server
- `site/version.json`: what the "deployed app" serves (`revision: a82f19c`)
- `with-postgres.yaml` + `checks/deploy.json`: HTTP plus PostgreSQL, for a
  real environment (it needs `AFTERGREEN_POSTGRES_URL`)

## Run it

```bash
cd examples/verification

# 1. Start the "deployed app" (any static file server works)
python3 -m http.server 8000 --bind 127.0.0.1 --directory site &

# 2. The revision that is live: VERIFIED, exit 0
DEPLOY_SHA=a82f19c aftergreen verify -f aftergreen.yaml

# 3. A revision that is not live: FAILED, exit 1.
#    service-up still passes: HTTP 200 alone doesn't prove the new build is serving.
DEPLOY_SHA=b7e01d4 aftergreen verify -f aftergreen.yaml

# 4. Forgot to set the variable: invalid input, exit 2, nothing checked
aftergreen verify -f aftergreen.yaml

# 5. Stop the server, then run again: UNKNOWN, exit 3
kill %1
DEPLOY_SHA=a82f19c aftergreen verify -f aftergreen.yaml
```

## Adapt it

1. Copy `aftergreen.yaml` into your repository.
2. Point `url` at an endpoint that serves your build ID.
3. Set `DEPLOY_SHA` (or rename the variable) in the CI step that runs
   `aftergreen verify` after the deploy.

Format reference: [docs/verify.md](../../docs/verify.md).
