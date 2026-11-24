# Contributing

Thanks for improving the project. Please open an issue before substantial work so that implementation direction is clear.

## Development

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build ./cmd/api
```

Keep the design small and dependency-light. Do not add an ORM, a cache service, or a message broker without a concrete workload reason. Add tests for behavior changes and update the README when an operational assumption changes.

## Pull requests

- Use focused commits and describe the problem, approach, and validation.
- Do not commit database files, credentials, R2 keys, or `.env` files.
- Preserve the explicit distinction between demonstration code and production-grade payment or authorization flows.
