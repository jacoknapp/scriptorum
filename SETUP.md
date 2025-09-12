# Scriptorum Setup Guide

This guide walks you through setting up Scriptorum from scratch, including all dependencies and configuration options.

## Prerequisites

Before installing Scriptorum, ensure you have the following:

### Required
- **Docker & Docker Compose** (recommended) OR **Go 1.21+** (for manual installation)
- **Readarr Instance(s)**: At least one Readarr installation for eBooks and/or audiobooks
- **Storage**: Adequate disk space for your book collections

### Optional
- **OAuth/OIDC Provider**: For single sign-on authentication (e.g., Authentik, Keycloak, Google, etc.)
- **Reverse Proxy**: For SSL termination and custom domains (e.g., Nginx, Traefik)

## Installation Methods

### Method 1: Docker Compose (Recommended)

1. **Create project directory**:
   ```bash
   mkdir scriptorum
   cd scriptorum
   ```

2. **Create docker-compose.yml**:
   ```yaml
   version: '3.8'
   services:
     scriptorum:
       image: scriptorum:latest  # Replace with actual image when available
       container_name: scriptorum
       ports:
         - "8080:8080"
       volumes:
         - ./data:/data
       environment:
         - CONFIG_PATH=/data/scriptorum.yaml
       restart: unless-stopped
   ```

3. **Start the service**:
   ```bash
   docker compose up -d
   ```

4. **Access the setup wizard**:
   Open `http://localhost:8080` in your browser

### Method 2: Manual Installation

1. **Install Go** (if not already installed):
   ```bash
   # On Ubuntu/Debian
   sudo apt update
   sudo apt install golang-go

   # On macOS
   brew install go

   # On Windows
   # Download from https://golang.org/dl/
   ```

2. **Clone and build**:
   ```bash
   git clone <repository-url>
   cd scriptorum
   go build -o scriptorum ./cmd/scriptorum
   ```

3. **Create data directory**:
   ```bash
   mkdir data
   ```

4. **Run Scriptorum**:
   ```bash
   ./scriptorum -config data/scriptorum.yaml
   ```

5. **Access the setup wizard**:
   Open `http://localhost:8080` in your browser

## Setup Wizard Configuration

The setup wizard guides you through the initial configuration. Here's what each step configures:

### Step 1: Administrator Account

Create your primary admin user:
- **Username**: Choose a username for the admin account
- **Password**: Set a secure password (store this safely!)

**Note**: This creates a local admin account. OAuth admins are configured separately.

### Step 2: OAuth Configuration (Optional)

If you have an OAuth/OIDC provider:
- **Enable OAuth**: Check to enable OAuth authentication
- **Issuer URL**: Your OAuth provider's issuer URL (e.g., `https://auth.example.com`)
- **Client ID**: OAuth application client ID
- **Client Secret**: OAuth application client secret
- **Redirect URL**: Should be `http://your-domain:8080/oauth/callback`

**OAuth Provider Examples**:

**Authentik**:
- Issuer: `https://authentik.example.com/application/o/scriptorum/`
- Scopes: `openid`, `profile`, `email`
- Username Claim: `preferred_username`

**Keycloak**:
- Issuer: `https://keycloak.example.com/realms/your-realm`
- Scopes: `openid`, `profile`, `email`
- Username Claim: `preferred_username`

**Google**:
- Issuer: `https://accounts.google.com`
- Scopes: `openid`, `profile`, `email`
- Username Claim: `email`

### Step 3: Readarr Configuration

Configure your Readarr instance(s):

#### eBooks Configuration
- **Base URL**: Full URL to your eBooks Readarr instance (e.g., `http://readarr:8787`)
- **API Key**: Readarr API key (found in Settings → General → Security)
- **Quality Profile ID**: Default quality profile for eBooks (usually 1)
- **Root Folder**: Default storage path for eBooks (e.g., `/books/ebooks`)

#### Audiobooks Configuration  
- **Base URL**: Full URL to your audiobooks Readarr instance (e.g., `http://audiobookarr:8787`)
- **API Key**: Readarr API key for audiobooks instance
- **Quality Profile ID**: Default quality profile for audiobooks (usually 2)
- **Root Folder**: Default storage path for audiobooks (e.g., `/books/audiobooks`)
- **Tags**: Default tags (e.g., `audiobook`)

**Finding Readarr Settings**:
1. Open Readarr web interface
2. Go to Settings → General
3. API Key is under Security section
4. Quality Profiles are under Settings → Quality Profiles
5. Root Folders are under Settings → Root Folders

### Step 4: Complete Setup

Review your configuration and complete the setup. The wizard will:
- Save your configuration to `data/scriptorum.yaml`
- Initialize the SQLite database at `data/scriptorum.db`
- Create your admin user account
- Mark setup as completed

## Post-Setup Configuration

### Adding Users

#### Via Web Interface (Admin Required)
1. Login as admin
2. Navigate to **Users** page
3. Click **Add User**
4. Fill in username, password, and admin status
5. Click **Add User**

#### Via OAuth Auto-Provisioning
Enable in config:
```yaml
oauth:
  auto_create_users: true
```

Users will be automatically created on first OAuth login.

### Managing Admin Users

#### Local Admin Users
Edit users via the web interface or modify the database directly.

#### OAuth Admin Users
Add usernames to the config file:
```yaml
admins:
  usernames:
    - "admin"
    - "john.doe"
    - "jane.smith"
```

Restart Scriptorum after making changes.

### Advanced Configuration

Edit `data/scriptorum.yaml` for advanced options:

```yaml
# Debug logging
debug: true

# Custom HTTP settings
http:
  listen: ":8080"
  timeout: "30s"

# Database settings
db:
  path: "data/scriptorum.db"
  max_connections: 10

# OAuth advanced settings
oauth:
  scopes: ["openid", "profile", "email", "groups"]
  username_claim: "preferred_username"
  cookie_domain: "example.com"
  cookie_secure: true
  cookie_secret: "your-secret-key"

# Readarr advanced settings
readarr:
  ebooks:
    insecure_skip_verify: true  # For self-signed certificates
    timeout: "30s"
  audiobooks:
    insecure_skip_verify: true
    timeout: "30s"

# Provider settings
amazon_public:
  enabled: true
  timeout: "10s"
```

## Reverse Proxy Configuration

### Nginx Example
```nginx
server {
    listen 80;
    server_name scriptorum.example.com;
    
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Traefik Example
```yaml
version: '3.8'
services:
  scriptorum:
    image: scriptorum:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.scriptorum.rule=Host(`scriptorum.example.com`)"
      - "traefik.http.routers.scriptorum.entrypoints=websecure"
      - "traefik.http.routers.scriptorum.tls.certresolver=letsencrypt"
```

## Troubleshooting Setup

### Common Issues

**"Setup already completed" message**
- Delete or edit `data/scriptorum.yaml` and set `setup.completed: false`
- Restart Scriptorum

**Database initialization errors**
- Ensure `data/` directory is writable
- Check disk space
- Verify SQLite is available

**Readarr connection failures**
- Verify Readarr is running and accessible
- Check API keys are correct and not expired
- Test connectivity: `curl http://readarr:8787/api/v1/system/status?apikey=YOUR_KEY`

**OAuth authentication issues**
- Verify issuer URL is correct and accessible
- Check client ID and secret
- Ensure redirect URL matches exactly
- Check OAuth provider logs for errors

**Port conflicts**
- Change port in config: `http.listen: ":8081"`
- Update Docker port mapping accordingly

### Log Analysis

Enable debug logging:
```yaml
debug: true
```

Common log messages:
- `Setup wizard is running` - First-time setup in progress
- `OAuth disabled: discovery failed` - OAuth configuration issue
- `Database migration completed` - Database setup successful
- `Readarr connection test failed` - Readarr connectivity issue

## Security Considerations

### Production Deployment
- Use HTTPS with proper SSL certificates
- Set secure cookie settings:
  ```yaml
  oauth:
    cookie_secure: true
    cookie_domain: "your-domain.com"
  ```
- Use strong, unique passwords
- Regularly update Scriptorum and dependencies
- Limit network access to Readarr APIs
- Enable OAuth for centralized authentication

### Backup Recommendations
- Backup `data/scriptorum.db` regularly
- Backup `data/scriptorum.yaml` configuration
- Consider database replication for high availability

## Next Steps

After setup completion:
1. **Test Search**: Try searching for books to verify provider connectivity
2. **Create Test Request**: Submit a book request to test the workflow
3. **Configure Users**: Set up additional users as needed
4. **Monitor Logs**: Watch logs for any issues or errors
5. **Setup Monitoring**: Consider monitoring disk space and service health

For ongoing usage, see the main README.md file for feature documentation and API references.
