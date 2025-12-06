# Contributing to NovaEdge

Thank you for your interest in contributing to NovaEdge! This guide covers how to get started.

## Getting Started

### Prerequisites

- Go 1.25 or higher
- make
- Docker (for building container images)
- A Kubernetes cluster (for testing)
- kubectl configured

### Setting Up Your Development Environment

```bash
# Clone the repository
git clone https://github.com/piwi3910/novaedge.git
cd novaedge

# Install dependencies
go mod download

# Build all components
make build-all

# Run tests
make test

# Run linter
make lint
```

## Development Workflow

### 1. Create an Issue

Before starting work, create a GitHub issue describing:

- **For bugs**: Steps to reproduce, expected vs actual behavior
- **For features**: Use case, proposed solution, alternatives considered

### 2. Fork and Branch

```bash
# Fork the repository on GitHub, then:
git clone https://github.com/YOUR-USERNAME/novaedge.git
cd novaedge
git remote add upstream https://github.com/piwi3910/novaedge.git

# Create a feature branch
git checkout -b feature/my-feature
```

### 3. Make Changes

Follow the coding standards in the [Development Guide](development-guide.md):

- Use structured logging with zap
- Propagate context through all functions
- Wrap errors with context
- Write tests for new functionality

### 4. Test Your Changes

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run integration tests
go test -v ./test/integration/...

# Run linter
make lint
```

### 5. Submit a Pull Request

```bash
# Push your branch
git push origin feature/my-feature
```

Then open a pull request on GitHub with:

- Clear description of changes
- Reference to related issue(s)
- Test plan or evidence of testing
- Screenshots for UI changes

## Code Standards

### Go Code

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting
- No linting errors (`make lint`)
- All tests pass (`make test`)

### Commit Messages

Use clear, descriptive commit messages:

```
[Type] Short summary (50 chars or less)

Detailed explanation of what and why (not how).
Wrap at 72 characters.

Resolves #123
```

Types: `[Feature]`, `[Fix]`, `[Refactor]`, `[Docs]`, `[Test]`

### Pull Request Checklist

- [ ] Tests added for new functionality
- [ ] All tests pass
- [ ] No linting errors
- [ ] Documentation updated if needed
- [ ] Commit messages follow guidelines
- [ ] PR description is complete

## Project Structure

```
novaedge/
├── api/v1alpha1/         # CRD type definitions
├── cmd/
│   ├── novaedge-agent/   # Agent main
│   ├── novaedge-controller/  # Controller main
│   └── novactl/          # CLI tool
├── config/
│   ├── crd/              # CRD manifests
│   ├── rbac/             # RBAC manifests
│   └── samples/          # Example resources
├── docs/                 # Documentation
├── internal/
│   ├── agent/            # Agent implementation
│   │   ├── config/       # Config handling
│   │   ├── health/       # Health checking
│   │   ├── lb/           # Load balancing
│   │   ├── policy/       # Policy enforcement
│   │   ├── router/       # Request routing
│   │   ├── upstream/     # Backend connections
│   │   └── vip/          # VIP management
│   ├── controller/       # Controller implementation
│   ├── pkg/              # Shared packages
│   └── proto/            # Protocol buffers
└── test/
    └── integration/      # Integration tests
```

## Testing

### Unit Tests

Write unit tests for all new code:

```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"valid input", "foo", "bar"},
        {"empty input", "", ""},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := MyFunction(tt.input)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### Integration Tests

Integration tests go in `test/integration/`:

```bash
# Run integration tests
go test -v ./test/integration/...
```

### Manual Testing

For manual testing with a local cluster:

```bash
# Build and load images
make docker-build
kind load docker-image novaedge:latest

# Deploy to cluster
make install-crds
kubectl apply -f config/

# Apply test resources
kubectl apply -f config/samples/
```

## Documentation

### Updating Docs

Documentation is in `docs/`:

- User-facing guides in `docs/user-guide/`
- Reference docs in `docs/reference/`
- Development docs in `docs/development/`

### Building Docs Locally

```bash
# Install mkdocs
pip install mkdocs-material

# Serve locally
mkdocs serve

# Build static site
mkdocs build
```

## Getting Help

- **GitHub Issues**: For bug reports and feature requests
- **Discussions**: For questions and community help
- **CLAUDE.md**: Development guidelines for AI assistance

## Code of Conduct

Be respectful and constructive in all interactions. We're building something together.

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
