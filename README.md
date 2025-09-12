# Scriptorum

A modern, Overseerr-style web application for managing eBook and audiobook requests. Scriptorum provides a beautiful royal-purple themed UI that bridges multiple book sources and integrates with Readarr for automated media management.

## 🌟 Features

### 📚 Multi-Source Book Discovery
- **Amazon Public Search**: Automatic ASIN detection and metadata scraping from public Amazon pages
- **Open Library Integration**: Keyword search with comprehensive metadata from Open Library
- **Intelligent Deduplication**: Automatically merges and deduplicates results from multiple sources
- **Rich Metadata**: Cover images, descriptions, publication details, and more

### 🎯 Request Management System
- **User-Friendly Requests**: Simple interface for users to request books and audiobooks
- **Admin Dashboard**: Comprehensive request management with approve/decline/delete capabilities
- **Bulk Operations**: Approve all pending requests or delete all requests with one click
- **Request Filtering**: Non-admin users see only their own requests; admins see everything
- **Status Tracking**: Real-time request status updates (pending, approved, declined, etc.)

### 👥 User Management & Authentication
- **Setup Wizard**: First-run setup wizard for easy initial configuration
- **Local Authentication**: Username/password authentication with secure password hashing
- **OAuth Integration**: Support for OIDC/OAuth providers (optional)
- **User Administration**: Create, edit, and delete users with admin privileges
- **Role-Based Access**: Granular permissions for regular users vs administrators
- **Auto-Provisioning**: Automatically create users from OAuth authentication

### 🔗 Readarr Integration
- **Dual Instance Support**: Separate Readarr instances for eBooks and audiobooks
- **Intelligent Matching**: ISBN-13 → ISBN-10 → ASIN fallback matching
- **Quality Profiles**: Configurable quality profiles for different media types
- **Root Folder Management**: Automatic organization into specified root folders
- **Tag Support**: Automatic tagging of requests (e.g., "audiobook" tag)
- **Request Hydration**: Retroactively attach selection payloads to older requests

### 🎨 Modern UI/UX
- **Responsive Design**: Works beautifully on desktop, tablet, and mobile
- **Dark Theme**: Easy-on-the-eyes dark theme with royal purple accents
- **Modal Dialogs**: Modern modal interfaces for forms and confirmations
- **HTMX Integration**: Dynamic updates without page refreshes
- **Accessibility**: Proper ARIA labels and keyboard navigation support

## 🚀 Quick Start

### Docker (Recommended)

```bash
# Clone the repository
git clone <repository-url>
cd scriptorum

# Start with Docker Compose
docker compose up -d --build

# Open the application
open http://localhost:8080
```

### Manual Installation

```bash
# Build from source
make build

# Run with custom config
./bin/scriptorum -config /path/to/config.yaml

# Or run tests
make test
```

## ⚙️ Configuration

### First-Run Setup

1. **Navigate to Setup Wizard**: Visit `http://localhost:8080` - you'll be redirected to the setup wizard
2. **Create Admin User**: Set up your local administrator account
3. **Configure OAuth** (Optional): Set up OIDC/OAuth integration for SSO
4. **Configure Readarr**: 
   - **eBooks**: Enter Readarr instance URL and API key for ebooks
   - **Audiobooks**: Enter Readarr instance URL and API key for audiobooks
5. **Complete Setup**: Finish the wizard to start using Scriptorum

### Configuration File

The configuration is stored in YAML format. See `scriptorum.example.yaml` for a complete example:

```yaml
# HTTP Server Configuration
http:
  listen: ":8080"

# Database Configuration
db:
  path: "/data/scriptorum.db"

# Authentication
auth:
  salt: "auto-generated-salt"

# Administrator Configuration
admins:
  usernames:
    - "admin"
    - "your-username"

# OAuth/OIDC Configuration (Optional)
oauth:
  enabled: true
  issuer: "https://your-auth-provider.com"
  client_id: "your-client-id"
  client_secret: "your-client-secret"
  redirect_url: "http://localhost:8080/oauth/callback"
  auto_create_users: true

# Readarr Integration
readarr:
  ebooks:
    base_url: "http://readarr-ebooks:8787"
    api_key: "your-ebooks-api-key"
    default_quality_profile_id: 1
    default_root_folder_path: "/books/ebooks"
  audiobooks:
    base_url: "http://readarr-audio:8787"
    api_key: "your-audiobooks-api-key"
    default_quality_profile_id: 2
    default_root_folder_path: "/books/audiobooks"
    default_tags: ["audiobook"]
```

## 🏗️ Project Structure

```
scriptorum/
├── cmd/scriptorum/          # Application entry point
├── internal/
│   ├── config/             # Configuration management
│   ├── db/                 # Database operations (SQLite)
│   ├── httpapi/            # HTTP API and web server
│   │   ├── web/
│   │   │   ├── static/     # CSS, JS, images
│   │   │   ├── templates/  # HTML templates
│   │   │   └── setup/      # Setup wizard templates
│   ├── providers/          # Book data providers (Amazon, Open Library)
│   ├── settings/           # Runtime settings management
│   └── util/               # Utility functions
├── data/                   # Runtime data (database, config)
├── docker-compose.yml      # Docker Compose configuration
├── Dockerfile              # Container build definition
└── Makefile               # Build automation
```

## 🛠️ Development

### Prerequisites
- Go 1.21 or later
- SQLite3
- Docker (for containerized development)

### Development Commands

```bash
# Build the application
make build

# Run tests
make test

# Run with development settings
make run

# Build and run with Docker
make docker-run

# Clean build artifacts
make clean
```

### API Endpoints

#### Authentication
- `GET /login` - Login page
- `POST /login` - Local authentication
- `GET /oauth/callback` - OAuth callback
- `GET /logout` - Logout

#### Request Management
- `GET /api/v1/requests` - List requests
- `POST /api/v1/requests` - Create request
- `POST /api/v1/requests/{id}/approve` - Approve request
- `POST /api/v1/requests/{id}/decline` - Decline request
- `DELETE /api/v1/requests/{id}` - Delete request
- `POST /api/v1/requests/approve-all` - Approve all pending
- `DELETE /api/v1/requests` - Delete all requests

#### Search
- `GET /search` - Search interface
- `GET /api/providers/search` - Search books across providers

#### User Management (Admin)
- `GET /users` - User management page
- `POST /users` - Create user
- `POST /users/edit` - Edit user
- `GET /users/delete` - Delete user

## 🔒 Security Features

- **Secure Password Hashing**: Uses bcrypt with configurable salt
- **Session Management**: Secure HTTP-only cookies with CSRF protection
- **Role-Based Access Control**: Granular permissions for different user types
- **OAuth Integration**: Support for enterprise SSO providers
- **Input Validation**: Comprehensive validation of all user inputs
- **SQL Injection Protection**: Parameterized queries throughout

## 📊 Request Filtering

### For Regular Users
- See only requests they have submitted
- Cannot see other users' requests
- Cannot access admin functions

### For Administrators
- See all requests from all users
- Can approve, decline, or delete any request
- Access to bulk operations (approve all, delete all)
- User management capabilities
- System configuration access

## 🐛 Troubleshooting

### Common Issues

**Setup Wizard Not Appearing**
- Check if `setup.completed` is set to `false` in your config file
- Ensure database is writable

**Readarr Connection Issues**
- Verify API keys are correct
- Check network connectivity between Scriptorum and Readarr instances
- Ensure base URLs include protocol (http/https)

**Authentication Problems**
- For OAuth issues, check issuer URL and client credentials
- Verify redirect URL matches OAuth provider configuration
- Check logs for detailed error messages

**Request Processing Issues**
- Ensure Readarr instances are properly configured
- Verify quality profiles and root folders exist
- Check that selection payloads are attached to requests

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/amazing-feature`
3. Commit your changes: `git commit -m 'Add amazing feature'`
4. Push to the branch: `git push origin feature/amazing-feature`
5. Open a Pull Request

## 📋 CI/CD

GitHub Actions automatically:
- Builds and tests Go code
- Builds multi-architecture Docker images
- Pushes to Docker Hub with tags: `latest`, short SHA, and release tags

Required secrets:
- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 🙏 Acknowledgments

- **Amazon** - Public search capabilities (no API keys required)
- **Open Library** - Comprehensive book metadata
- **Readarr** - Media management integration
- **HTMX** - Modern web interactions without JavaScript complexity
