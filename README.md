# Scriptorum — Book request manager

[![Go 1.25+](https://img.shields.io/badge/go-1.25+-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
Lightweight, self-hosted web app to manage eBook and audiobook requests. Scriptorum aggregates book metadata from multiple sources (Amazon public pages, Open Library, Readarr) and helps you queue titles into Readarr with convenient approvals, notifications, and a simple UI.

This README focuses on getting you running quickly (Docker + native), explains configuration, and lists the most useful operational tips.
## Quick pointers (skip to the relevant section below)
- Docker: recommended for most users — see "Docker Quick Start"
- Native: use the included PowerShell helper on Windows — see "Run locally"
- Config file: `data/scriptorum.yaml` (created on first run if absent)
- Default HTTP listen port: `:8080` (docker-compose in this repo maps `8491` → see `docker-compose.yml`)
- Health endpoints: `/healthz` and `/version`
## Docker quick start (recommended)

1. Clone the repository:

```powershell
git clone https://github.com/your-username/scriptorum.git
cd scriptorum
```

2. Start with Docker Compose (builds image if needed):

```powershell
docker compose up -d --build
```

3. Open the UI in your browser. By default the app listens on `:8080` inside the container. The included `docker-compose.yml` maps port `8491` on the host in this repo; open:

http://localhost:8491

Notes
- If you change ports in `scriptorum.yaml`, adjust the compose mapping.
- To stop and remove containers:

```powershell
docker compose down
```
## Run locally (development / advanced)

Use the included PowerShell helper on Windows (also works on other shells with Go installed).

Prerequisites
- Go 1.25+
- SQLite3 (optional; the binary embeds modernc SQLite driver)
- Node.js / npm (optional for rebuilding CSS)

Build and run using the helper script:

```powershell
# build the binary
.\build.ps1 build

# run tests
.\build.ps1 test

# run the app (builds first if needed)
.\build.ps1 run
```

Or build and run directly with Go:

```powershell
go build -o ./bin/scriptorum ./cmd/scriptorum
./bin/scriptorum
```

By default the app will create `data/scriptorum.yaml` and `data/scriptorum.db` on first run if they don't exist. You can override locations with environment variables (PowerShell example):

```powershell
$env:SCRIPTORUM_CONFIG_PATH = "C:\data\scriptorum.yaml"
$env:SCRIPTORUM_DB_PATH = "C:\data\scriptorum.db"
```
## Configuration highlights

- Example config: `scriptorum.example.yaml` (repo root). Copy it to `data/scriptorum.yaml` and edit.
- Important sections:
  - `http.listen` — default `:8080`
  - `db.path` — SQLite DB location
  - `readarr.ebooks` / `readarr.audiobooks` — base_url and api_key for Readarr integrations
  - `notifications` — ntfy, smtp, discord options
  - `oauth` — optional OIDC provider settings

Minimal example (already present in repo as `scriptorum.example.yaml`):

```yaml
http:
  listen: ":8080"
db:
  path: "data/scriptorum.db"
readarr:
  ebooks:
    base_url: "http://readarr-ebooks:8787"
    api_key: ""
  audiobooks:
    base_url: "http://readarr-audio:8787"
    api_key: ""
admins:
  usernames: ["admin"]
```

After adjusting `data/scriptorum.yaml`, restart the service/container.
## Health & troubleshooting

- Health check: `GET /healthz` (returns 200 when healthy)
- Version: `GET /version`
- Logs: container logs (`docker compose logs -f scriptorum`) or stdout when running locally

Common issues
- Setup wizard not appearing: ensure `setup.completed` in config is `false` (first run) and the DB file is writable.
- Readarr connectivity: verify API keys, base URLs include `http://` or `https://`, and the Readarr instance is reachable from the Scriptorum host.
- OAuth problems: confirm redirect URL configured on your provider matches `oauth.redirect_url` in config.

Enable debug logging in config:

```yaml
debug: true
```

## Developer notes

- Frontend styles: built with Tailwind. To rebuild CSS locally:

```powershell
npm install
npm run build:css
# or to watch during development
npm run watch:css
```

- Tests: `go test ./...` (the repo includes a PowerShell helper `.\build.ps1 test`).

- Project layout (most relevant folders):
  - `cmd/scriptorum` — app entrypoint
  - `internal/httpapi` — web server, templates & static assets
  - `internal/providers` — Amazon/OpenLibrary/Readarr providers
  - `data/` — runtime config + DB (created automatically)

## API

Full API documentation is included in `API.md` (endpoints, auth, examples).

## Contributing

Contributions welcome. Please open issues and PRs on the GitHub repo. Follow standard practice: branch, test, document.

## License

MIT — see the `LICENSE` file in this repository.

---

If you'd like, I can also:
- add a short example `docker-compose.override.yml` for a reference Readarr + Scriptorum stack
- trim the README into a shorter quick-start only version for the repo root

What would you like next?
# Scriptorum

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://golang.org)
[![Docker](https://img.shields.io/badge/docker-ready-blue.svg)](https://www.docker.com/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![GitHub Actions](https://img.shields.io/github/actions/workflow/status/your-username/scriptorum/ci.yml)](https://github.com/your-username/scriptorum/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/your-username/scriptorum)](https://goreportcard.com/report/github.com/your-username/scriptorum)

> A modern, self-hosted web application for managing eBook and audiobook requests with seamless Readarr integration.

Scriptorum provides a beautiful dark-themed interface that aggregates book data from multiple sources and automates media acquisition workflows. Perfect for media servers, libraries, and book enthusiasts who want organized, automated book management.

## 📋 Table of Contents

- [✨ Key Features](#-key-features)
- [🚀 Quick Start](#-quick-start)
- [📋 System Requirements](#-system-requirements)
- [⚙️ Configuration](#️-configuration)
- [🏗️ Architecture](#️-architecture)
- [🛠️ Development](#️-development)
- [🔒 Security](#-security)
- [📊 Usage Guide](#-usage-guide)
- [🐛 Troubleshooting](#-troubleshooting)
- [🤝 Contributing](#-contributing)
- [📋 CI/CD](#-ci/cd)
- [❓ FAQ](#-faq)
- [📄 License](#-license)

## ✨ Key Features

### 📚 Intelligent Book Discovery
- **🔍 Multi-Source Search**: Aggregate results from Amazon Public Search and Open Library
- **🧠 Smart Deduplication**: Automatically merges duplicate entries across providers
- **📖 Rich Metadata**: Comprehensive book information including covers, descriptions, and publication details
- **🎯 ASIN Detection**: Automatic Amazon Standard Identification Number extraction from public pages

### 🎯 Request Management System
- **👤 User-Friendly Interface**: Intuitive request submission with real-time validation
- **⚡ Admin Dashboard**: Powerful management tools with approve/decline/delete capabilities
- **📦 Bulk Operations**: Approve all pending requests or clear all requests with single actions
- **🔒 Role-Based Filtering**: Users see only their requests; admins see everything
- **📊 Status Tracking**: Live request status updates with comprehensive history

### 👥 Authentication & User Management
- **🧙‍♂️ First-Run Setup Wizard**: Guided initial configuration process
- **🔐 Local Authentication**: Secure username/password with bcrypt hashing
- **🔑 OAuth/OIDC Integration**: Enterprise SSO support with auto-provisioning
- **👮 Granular Permissions**: Role-based access control for users vs administrators
- **👥 User Administration**: Complete CRUD operations for user accounts

### 🔗 Readarr Integration
- **📚 Dual Instance Support**: Separate configurations for eBooks and audiobooks
# 1. Clone the repository
# 3. Access the application
- SQLite3

- 512MB available RAM
    api_key: "your-ebooks-api-key"
# Scriptorum — Book request manager

[![Go 1.25+](https://img.shields.io/badge/go-1.25+-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Lightweight, self-hosted web app to manage eBook and audiobook requests. Scriptorum aggregates book metadata from multiple sources (Amazon public pages, Open Library, Readarr) and helps you queue titles into Readarr with convenient approvals, notifications, and a simple UI.

This README focuses on getting you running quickly (Docker + native), explains configuration, and lists the most useful operational tips.

---

## Quick pointers

- Docker: recommended for most users — see "Docker quick start"
- Native: use the included PowerShell helper on Windows — see "Run locally"
- Config file: `data/scriptorum.yaml` (created on first run if absent)
- Default HTTP listen port: `:8080` (note: `docker-compose.yml` in this repo exposes host port `8491`)
- Health endpoints: `/healthz` and `/version`

---

## Docker quick start (recommended)

1. Clone the repository:

```powershell
git clone https://github.com/your-username/scriptorum.git
cd scriptorum
```

2. Start with Docker Compose (builds the image if needed):

```powershell
docker compose up -d --build
```

3. Open the UI in your browser. The repo's compose file maps host port `8491` → container port `8080`:

http://localhost:8491

Notes
- If you change `http.listen` in your config, update the compose port mapping.
- To view logs:

```powershell
docker compose logs -f scriptorum
```

- To stop and remove containers:

```powershell
docker compose down
```

---

## Run locally (development / advanced)

Use the included PowerShell helper on Windows (works on other platforms with Go installed).

Prerequisites
- Go 1.25+
- SQLite3 (optional; the binary uses modernc SQLite driver)
- Node.js / npm (optional for rebuilding CSS)

Build and run using the helper script:

```powershell
# build the binary
.\build.ps1 build

# run tests
.\build.ps1 test

# run the app (builds first if needed)
.\build.ps1 run
```

Or build and run directly with Go:

```powershell
go build -o ./bin/scriptorum ./cmd/scriptorum
./bin/scriptorum
```

By default the app will create `data/scriptorum.yaml` and `data/scriptorum.db` on first run if they don't exist. Override paths with environment variables (PowerShell example):

```powershell
$env:SCRIPTORUM_CONFIG_PATH = "C:\data\scriptorum.yaml"
$env:SCRIPTORUM_DB_PATH = "C:\data\scriptorum.db"
```

---

## Configuration highlights

- Example config: `scriptorum.example.yaml` (repo root). Copy it to `data/scriptorum.yaml` and edit.
- Important keys:
  - `http.listen` — default `:8080`
  - `db.path` — SQLite DB location
  - `readarr.ebooks` / `readarr.audiobooks` — base_url and api_key
  - `notifications` — settings for ntfy, smtp, discord
  - `oauth` — OIDC provider settings (optional)

Minimal example (trimmed):

```yaml
http:
  listen: ":8080"
db:
  path: "data/scriptorum.db"
readarr:
  ebooks:
    base_url: "http://readarr-ebooks:8787"
    api_key: ""
  audiobooks:
    base_url: "http://readarr-audio:8787"
    api_key: ""
admins:
  usernames: ["admin"]
```

After changing `data/scriptorum.yaml`, restart the app or container.

---

## Health & troubleshooting

- Health: `GET /healthz` (200 when healthy)
- Version: `GET /version`
- Logs: `docker compose logs -f scriptorum` or stdout when running locally

Common issues
- Setup wizard not showing: ensure `setup.completed` is `false` in config and the database file is writable.
- Readarr connectivity: verify API keys and reachable base_url (include protocol `http://` or `https://`).
- OAuth problems: check that the provider redirect URL matches `oauth.redirect_url`.

Enable debug:

```yaml
debug: true
```

---

## Developer notes

- CSS: Tailwind is used for styles. To rebuild locally:

```powershell
npm install
npm run build:css
# or watch
npm run watch:css
```

- Tests: `go test ./...` (or use `.\build.ps1 test`).

- Key folders:
  - `cmd/scriptorum` — application entrypoint
  - `internal/httpapi` — web server, templates, static assets
  - `internal/providers` — Amazon/OpenLibrary/Readarr providers
  - `data/` — runtime config + DB (created automatically)

---

## API

Full API reference is in `API.md` — endpoints, authentication, request/response examples.

---

## Contributing

Contributions welcome. Please open issues and PRs on GitHub. Use feature branches, include tests, and document changes.

---

## License

MIT — see the `LICENSE` file in this repository.

---

If you'd like, I can also:
- add a small `docker-compose.override.yml` example wiring a Readarr instance for testing
- produce a 1-page quickstart-to-the-point README

Which one should I do next?
export SCRIPTORUM_DB_PATH="/custom/path/database.db"
```

## 🏗️ Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Web Browser   │    │   Scriptorum    │    │    Readarr      │
│                 │    │   Web Server    │    │   Instances     │
│ • HTML/HTMX     │◄──►│ • REST API      │◄──►│ • eBooks        │
│ • Dark Theme    │    │ • SQLite DB     │    │ • Audiobooks    │
│ • Responsive    │    │ • OAuth/SSO     │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  Book Sources   │    │ Notifications   │    │   File System   │
│                 │    │                 │    │                 │
│ • Amazon        │◄──►│ • Ntfy          │    │ • Organized     │
│ • Open Library  │    │ • SMTP Email    │    │ • Root Folders  │
│ • Deduplication │    │ • Discord       │    │ • Metadata      │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### Core Components

- **HTTP Server**: Chi router-based web server with HTMX integration
- **Database**: SQLite with modernc driver for cross-platform compatibility
- **Authentication**: Local + OAuth/OIDC with secure session management
- **Providers**: Modular book data sources with unified interface
- **Notifications**: Event-driven notification system with multiple backends

## 🛠️ Development

### Prerequisites
- **Go**: 1.25 or later
- **Node.js**: For CSS compilation (optional)
- **SQLite3**: Database engine

### Development Workflow

```bash
# Clone the repository
git clone https://github.com/your-username/scriptorum.git
cd scriptorum

# Install frontend dependencies (optional)
npm install

# Build CSS (optional - uses pre-built if not run)
npm run build:css

# Run tests
go test ./...

# Build application
go build -o ./bin/scriptorum ./cmd/scriptorum

# Run with development settings
./bin/scriptorum
```

### PowerShell Build Script (Windows)

```powershell
# Build the application
.\build.ps1 build

# Run tests
.\build.ps1 test

# Build and run
.\build.ps1 run

# Clean build artifacts
.\build.ps1 clean
```

### CSS Development

```bash
# Watch CSS changes during development
npm run watch:css

# Build production CSS
npm run build:css
```

## 🔒 Security Features

- **🔐 Secure Password Hashing**: bcrypt with configurable salt
- **🍪 Session Management**: HTTP-only cookies with CSRF protection
- **🛡️ CSRF Protection**: Anti-forgery tokens on state-changing requests
- **👮 Role-Based Access**: Granular permissions for different user types
- **🔑 OAuth Integration**: Secure enterprise SSO providers
- **✅ Input Validation**: Comprehensive sanitization and validation
- **🗃️ SQL Injection Prevention**: Parameterized queries throughout
- **🔒 Security Headers**: CSP, HSTS, X-Frame-Options, and more

## 📊 Usage Guide

### For Regular Users
- **📝 Request Submission**: Search and request books through the intuitive interface
- **📊 Request Tracking**: Monitor personal request status and history
- **🔒 Limited Access**: Cannot view other users' requests or access admin functions

### For Administrators
- **⚡ Request Management**: Approve, decline, or delete any request
- **📦 Bulk Operations**: Process multiple requests simultaneously
- **👥 User Administration**: Manage user accounts and permissions
- **⚙️ System Configuration**: Access all settings and configuration options
- **🔔 Notification Testing**: Verify notification delivery

## 🐛 Troubleshooting

### Common Issues

**❌ Setup Wizard Won't Load**
- Verify `setup.completed` is `false` in configuration
- Ensure database file is writable
- Check file permissions on configuration directory

**🔌 Readarr Connection Failed**
- Validate API keys are correct and have proper permissions
- Confirm base URLs include protocol (http/https)
- Test network connectivity between services
- Verify Readarr instances are running and accessible

**🔐 Authentication Problems**
- For OAuth: Verify issuer URL, client credentials, and redirect URI
- Check OAuth provider configuration matches Scriptorum settings
- Review application logs for detailed error messages
- Confirm username claim mapping is correct

**🔍 Book Search Issues**
- Ensure Amazon Public Search is enabled (default)
- Check network connectivity to external services
- Verify Open Library API accessibility
- Review browser console for JavaScript errors

**📢 Notification Failures**
- Test notification provider credentials
- Verify webhook URLs or SMTP settings
- Check provider-specific rate limits
- Review notification logs in application

### Debug Mode

Enable debug logging by setting the debug flag in configuration:

```yaml
debug: true
```

Debug logs provide detailed information about:
- Provider search operations
- Readarr API interactions
- Authentication flows
- Notification delivery

### Health Checks

```bash
# Application health
curl http://localhost:8080/healthz

# Version information
curl http://localhost:8080/version
```

## 🤝 Contributing

We welcome contributions! Please follow these steps:

1. **🍴 Fork** the repository
2. **🌿 Create** a feature branch: `git checkout -b feature/amazing-feature`
3. **💻 Commit** changes: `git commit -m 'Add amazing feature'`
4. **📤 Push** to branch: `git push origin feature/amazing-feature`
5. **🔀 Open** a Pull Request

### Development Guidelines
- Follow Go best practices and conventions
- Add tests for new functionality
- Update documentation for API changes
- Ensure code passes all tests before submitting
- Use meaningful commit messages

### Community
- **🐛 Issues**: [GitHub Issues](https://github.com/your-username/scriptorum/issues)
- **💬 Discussions**: [GitHub Discussions](https://github.com/your-username/scriptorum/discussions)
- **📖 Documentation**: [API Reference](./API.md)

## 📋 CI/CD

GitHub Actions automatically:
- **🔨 Builds** Go binaries for multiple platforms
- **🧪 Runs** comprehensive test suite
- **🐳 Builds** multi-architecture Docker images
- **📦 Pushes** images with semantic versioning

### Required Secrets
- `DOCKERHUB_USERNAME`: Docker Hub username
- `DOCKERHUB_TOKEN`: Docker Hub access token

### Release Process
1. Create a Git tag with semantic versioning: `git tag v1.2.3`
2. Push the tag: `git push origin v1.2.3`
3. CI/CD pipeline builds and publishes release artifacts

## ❓ FAQ

### General Questions

**Q: Is Scriptorum free?**
A: Yes! Scriptorum is open-source and licensed under MIT.

**Q: Does it require Readarr?**
A: Readarr integration is optional but recommended for automated media acquisition.

**Q: Can I use it without Docker?**
A: Yes, Scriptorum can be built and run natively on Linux, macOS, and Windows.

**Q: How does it compare to other request systems?**
A: Scriptorum focuses specifically on books with rich metadata and Readarr integration.

### Technical Questions

**Q: Can I run multiple instances?**
A: Yes, each instance uses its own database and configuration.

**Q: What databases are supported?**
A: Currently SQLite only. PostgreSQL support may be added in future versions.

**Q: Can I customize the UI theme?**
A: The theme is built with Tailwind CSS and can be customized by modifying the source.

**Q: Are there API rate limits?**
A: Scriptorum respects external API limits and includes configurable delays.

### Integration Questions

**Q: Which Readarr versions are supported?**
A: Compatible with Readarr v0.1.0+ and Sonarr v3+ (for audiobooks).

**Q: Can I use other book sources?**
A: The provider system is extensible. Additional sources can be added.

**Q: What OAuth providers are supported?**
A: Any OIDC-compliant provider (Auth0, Keycloak, Google, etc.).

## 📄 API Documentation

Comprehensive API documentation is available in [`API.md`](./API.md), including:
- Complete endpoint reference
- Authentication methods
- Request/response examples
- Error handling
- Permission requirements

## 🔗 Integrations

### Readarr
- **📚 eBooks**: Manage electronic book collections
- **🎧 Audiobooks**: Handle spoken word content
- **⚙️ Quality Profiles**: Configurable download quality
- **📁 Root Folders**: Organized storage structure
- **🏷️ Tags**: Metadata tagging for organization

### Notification Providers
- **📱 Ntfy**: Lightweight notification service
- **📧 SMTP**: Traditional email notifications
- **💬 Discord**: Team communication integration

### Authentication Providers
- **🔑 OAuth 2.0/OIDC**: Enterprise SSO solutions
- **👥 Auto-Provisioning**: Automatic user creation
- **🏢 Domain Restrictions**: Email domain whitelisting

 
This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

**Made with ❤️ for the self-hosted media community**