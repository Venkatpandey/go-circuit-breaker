# Contributing

## Local workflow

```bash
make test
make test-race
make build
```

Use `make test-integration` when you want to run the Redis adapter suite.

## Project shape

- `core` contains the state machine and execution API
- `service` contains the named breaker manager
- `adapters` contains optional integrations
- `cmd/http-demo` contains the demo binary

## Contribution guidelines

- Keep the library local-first; do not introduce Redis or network dependencies into the hot path by default.
- Favor context-first APIs.
- Do not add library logging side effects.
- Keep the half-open concurrency contract explicit and covered by tests.
- When changing docs, make sure the README matches the code and build targets exactly.
- Add GoDoc comments for exported package/type/function/method changes.
- Use lightweight section-header comments in long files to improve scanability.
- Keep observer callbacks synchronous and cheap; heavy work should be offloaded by the observer implementation.
