# Contributing to API Infrastructure

## Development Setup

1. **Clone the repository**
   ```bash
   git clone <repo-url>
   cd api
   ```

2. **Initialize project**
   ```bash
   make init
   ```

3. **Configure environment**
   Edit `.env` with your local settings

4. **Start development environment**
   ```bash
   make dev
   ```

## Project Structure

```
api/
├── .github/
│   └── workflows/          # CI/CD pipelines
├── database/
│   └── init/              # Database initialization scripts
├── docs/                  # Documentation
├── hasura/
│   └── metadata/          # Hasura configuration
├── services/
│   ├── core/             # Node.js core service
│   ├── media/            # Go media service
│   └── health/           # Python health aggregator
├── traefik/
│   └── dynamic/          # Traefik configuration
├── docker-compose.yml    # Development compose
├── docker-compose.prod.yml  # Production overrides
├── Makefile              # Useful commands
└── README.md
```

## Making Changes

### Adding a New Service

1. Create service directory under `services/`
2. Add Dockerfile
3. Update `docker-compose.yml` with Traefik labels
4. Add routes and middleware
5. Create OpenAPI spec
6. Update GitHub Actions workflow
7. Update documentation

### Modifying Existing Service

1. Make changes in service directory
2. Run tests locally
3. Update OpenAPI spec if API changed
4. Update documentation
5. Create pull request

## Code Style

### TypeScript (Core Service)
- Follow existing ESLint configuration
- Use Prettier for formatting
- Write JSDoc comments for exported functions

### Go (Media Service)
- Follow standard Go formatting (`go fmt`)
- Use `go vet` for static analysis
- Write godoc comments for exported functions

### Python (Health Service)
- Follow PEP 8
- Use type hints
- Keep it simple (single file)

## Testing

### Core Service
```bash
cd services/core
npm test
```

### Media Service
```bash
cd services/media
go test -v ./...
```

### Integration Tests
```bash
# Start services
make dev

# Run integration tests
./scripts/integration-test.sh
```

## Documentation

- Update `docs/API.md` for API changes
- Update `docs/DEPLOYMENT.md` for infrastructure changes
- Update `docs/ARCHITECTURE.md` for design changes
- Keep README.md up to date

## Pull Request Process

1. **Fork the repository**
2. **Create a feature branch**
   ```bash
   git checkout -b feature/your-feature
   ```
3. **Make your changes**
4. **Test locally**
   ```bash
   make test
   ```
5. **Commit with clear messages**
   ```bash
   git commit -m "feat: add new endpoint for X"
   ```
6. **Push to your fork**
   ```bash
   git push origin feature/your-feature
   ```
7. **Create pull request**

## Commit Message Format

Follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation changes
- `style:` - Code style changes (formatting)
- `refactor:` - Code refactoring
- `perf:` - Performance improvements
- `test:` - Test additions/changes
- `chore:` - Build process or auxiliary tool changes
- `ci:` - CI/CD changes

Examples:
```
feat(core): add user profile endpoint
fix(media): handle large file uploads
docs: update API documentation
```

## Review Process

1. All PRs require at least one approval
2. CI/CD must pass (tests, linting, security scans)
3. Documentation must be updated
4. No merge conflicts

## Security

- Never commit secrets or credentials
- Use `.env` for local configuration
- Report security issues privately
- Follow OWASP guidelines

## Questions?

- Open an issue for bugs
- Start a discussion for features
- Check existing documentation first

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.
