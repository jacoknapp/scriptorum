# API Documentation

This document describes the Scriptorum REST API endpoints and their usage. Scriptorum provides a comprehensive API for managing book requests, user administration, system configuration, and integration with external services like Readarr.

## API Overview

The Scriptorum API is organized into several categories:

- **Request Management** (`/api/v1/requests/*`): Create, approve, decline, and manage book requests
- **Book Details** (`/api/v1/book/*`): Retrieve normalized book metadata from various sources
- **Search** (`/api/providers/search`): Search for books across multiple providers
- **Readarr Integration** (`/api/readarr/*`): Access Readarr quality profiles and root folders
- **Notifications** (`/api/notifications/*`): Test notification delivery (ntfy, SMTP, Discord)
- **User Management** (`/users/*`): Admin endpoints for user administration
- **Settings** (`/settings/*`): System configuration management
- **System** (`/healthz`, `/version`): Health checks and version information
- **UI** (`/ui/*`): HTMX-powered dynamic UI fragments
- **Approval Tokens** (`/approve/*`): One-click approval from notification links

## Authentication

Scriptorum supports two authentication methods:

### Session Cookies
- Login via `/login` endpoint to receive session cookie
- Cookie is automatically included in subsequent requests
- Used by the web interface

### OAuth/OIDC
- Redirect to `/oauth/login` to initiate OAuth flow
- Callback handled at `/oauth/callback`
- Session cookie set after successful authentication

## Base URL
All API endpoints are relative to your Scriptorum base URL:
```
https://your-scriptorum-instance.com/
```

**Note:** Some endpoints use different base paths:
- REST API endpoints: `/api/v1/`
- UI fragments: `/ui/`
- Admin endpoints: `/settings/`, `/users/`, `/notifications/`
- System endpoints: `/healthz`, `/version`

## Permissions & Access Control

Scriptorum implements role-based access control:

### Public Endpoints
- `GET /healthz` - No authentication required
- `GET /version` - No authentication required
- `GET /approve/{token}` - Uses secure token instead of authentication

### User Endpoints (Authenticated Users)
- `GET /api/v1/requests` - List user's own requests
- `POST /api/v1/requests` - Create new requests
- `POST /api/v1/book/*` - Access book details
- `GET /api/providers/search` - Search for books
- `GET /ui/*` - UI fragments and pages

### Admin-Only Endpoints
- `POST /api/v1/requests/{id}/approve` - Approve requests
- `POST /api/v1/requests/{id}/decline` - Decline requests
- `DELETE /api/v1/requests/{id}` - Delete requests
- `POST /api/v1/requests/{id}/hydrate` - Hydrate requests
- `POST /api/v1/requests/approve-all` - Bulk approve
- `DELETE /api/v1/requests` - Delete all requests
- `GET /api/readarr/debug` - Debug Readarr config
- `POST /api/notifications/test-*` - Test notifications
- `GET /settings` - Settings page
- `POST /settings/save` - Save settings
- `GET /users` - User management page
- `POST /users` - Create users
- `POST /users/edit` - Edit users
- `GET /users/delete` - Delete users
- `GET /notifications` - Notification settings page
- `POST /notifications/save` - Save notification settings

## Error Responses
All endpoints return standard HTTP status codes:
- `200` - Success
- `400` - Bad Request
- `401` - Unauthorized
- `403` - Forbidden
- `404` - Not Found
- `500` - Internal Server Error

Error responses include a message:
```json
{
  "error": "Description of the error"
}
```

## Endpoints

### Authentication Endpoints

#### POST /login
Local username/password authentication.

**Request Body:**
```json
{
  "username": "string",
  "password": "string"
}
```

**Response:**
- `302` - Redirect to dashboard on success
- `401` - Invalid credentials

#### GET /oauth/login
Initiates OAuth authentication flow.

**Response:**
- `302` - Redirect to OAuth provider

#### GET /oauth/callback
OAuth callback endpoint (handled automatically).

**Query Parameters:**
- `code` - Authorization code from OAuth provider
- `state` - State parameter for CSRF protection

#### GET /logout
Logs out the current user.

**Response:**
- `302` - Redirect to login page

### Request Management

#### GET /api/v1/requests
List requests based on user permissions.

**Query Parameters:**
- `limit` - Maximum number of results (default: 200)

**Response:**
```json
[
  {
    "id": 1,
    "title": "Book Title",
    "authors": ["Author Name"],
    "requester_email": "user@example.com",
    "status": "pending",
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-01T00:00:00Z",
    "kind": "ebook",
    "readarr_req": {
      "title": "Book Title",
      "author": "Author Name",
      "isbn": "1234567890",
      "asin": "B123456789"
    }
  }
]
```

**Permissions:**
- Regular users: Only see their own requests
- Admins: See all requests

#### POST /api/v1/requests
Create a new request.

**Request Body:**
```json
{
  "title": "Book Title",
  "authors": ["Author Name"],
  "kind": "ebook",
  "selection": {
    "title": "Book Title",
    "author": "Author Name",
    "isbn": "1234567890",
    "asin": "B123456789",
    "cover_url": "https://example.com/cover.jpg",
    "description": "Book description"
  }
}
```

**Response:**
```json
{
  "id": 123,
  "status": "created"
}
```

#### POST /api/v1/requests/{id}/approve
Approve a pending request (admin only).

**Path Parameters:**
- `id` - Request ID

**Response:**
```json
{
  "status": "queued"
}
```

**Notes:**
- Sends request to appropriate Readarr instance
- Requires request to have valid selection payload
- Only pending requests can be approved

#### POST /api/v1/requests/{id}/decline
Decline a pending request (admin only).

**Path Parameters:**
- `id` - Request ID

**Response:**
```json
{
  "status": "declined"
}
```

#### DELETE /api/v1/requests/{id}
Delete a request (admin only).

**Path Parameters:**
- `id` - Request ID

**Response:**
```json
{
  "status": "deleted"
}
```

#### POST /api/v1/requests/{id}/hydrate
Attempt to attach selection payload to a request (admin only).

**Path Parameters:**
- `id` - Request ID

**Response:**
```json
{
  "status": "hydrated"
}
```

**Notes:**
- Useful for older requests created before selection payloads
- Queries Readarr for book metadata based on stored identifiers
- May not always find a match

#### POST /api/v1/requests/approve-all
Approve all pending requests (admin only).

**Response:**
```json
{
  "status": "approved 5 requests"
}
```

**Notes:**
- Only approves requests with valid selection payloads
- Useful for bulk processing

#### DELETE /api/v1/requests
Delete all requests (admin only).

**Response:**
```json
{
  "status": "all requests deleted"
}
```

**⚠️ Warning:** This permanently deletes ALL requests. Use with caution.

### Book Details Endpoints

#### POST /api/v1/book/details
Get normalized book details from various sources.

**Request Body (JSON or Form Data):**
```json
{
  "provider_payload": "provider-specific data",
  "provider_payload_ebook": "ebook provider data",
  "provider_payload_audiobook": "audiobook provider data",
  "isbn13": "9781234567890",
  "isbn10": "1234567890",
  "asin": "B123456789",
  "title": "Book Title",
  "authors": ["Author Name"]
}
```

**Response:**
```json
{
  "title": "Book Title",
  "authors": ["Author Name"],
  "isbn10": "1234567890",
  "isbn13": "9781234567890",
  "asin": "B123456789",
  "cover": "https://example.com/cover.jpg",
  "description": "Book description",
  "provider_payload": "normalized provider data"
}
```

**Notes:**
- Accepts multiple input formats (JSON or form data)
- Normalizes author data to string arrays
- Returns 404 if no details found

#### POST /api/v1/book/enriched
Get enriched book details with additional metadata.

**Request Body (JSON or Form Data):**
```json
{
  "provider_payload": "provider-specific data",
  "isbn13": "9781234567890",
  "title": "Book Title",
  "authors": ["Author Name"]
}
```

**Response:**
```json
{
  "title": "Book Title",
  "authors": ["Author Name"],
  "isbn10": "1234567890",
  "isbn13": "9781234567890",
  "asin": "B123456789",
  "cover_url": "https://example.com/cover.jpg",
  "description": "Book description",
  "publication_date": "2025-01-01",
  "page_count": 300,
  "language": "en",
  "genres": ["Fiction"],
  "series": "Series Name",
  "series_index": 1
}
```

**Notes:**
- Provides richer metadata than basic details endpoint
- Includes publication info, genres, and series data

### Search Endpoints

#### GET /api/providers/search
Search for books across all enabled providers.

**Query Parameters:**
- `q` - Search query (required)
- `kind` - Media type: `ebooks`, `audiobooks`, or `both` (default: `both`)

**Response:**
```json
{
  "results": [
    {
      "title": "Book Title",
      "author": "Author Name",
      "isbn": "1234567890",
      "asin": "B123456789",
      "cover_url": "https://example.com/cover.jpg",
      "description": "Book description",
      "publication_date": "2025-01-01",
      "page_count": 300,
      "language": "en",
      "provider": "amazon",
      "url": "https://amazon.com/dp/B123456789"
    }
  ],
  "provider_results": {
    "amazon": 10,
    "openlibrary": 5
  }
}
```

**Notes:**
- Results are automatically deduplicated
- Amazon results are prioritized for metadata quality
- Cover images are proxied through Scriptorum

#### GET /api/readarr/profiles
Get Readarr quality profiles.

**Query Parameters:**
- `kind` - `ebooks` or `audiobooks` (required)

**Response:**
```json
[
  {
    "id": 1,
    "name": "Standard",
    "cutoff": {
      "id": 1,
      "name": "PDF"
    }
  }
]
```

#### GET /api/readarr/folders
Get Readarr root folders.

**Query Parameters:**
- `kind` - `ebooks` or `audiobooks` (required)

**Response:**
```json
[
  {
    "id": 1,
    "path": "/books/ebooks",
    "freespace": 1000000000,
    "totalspace": 2000000000
  }
]
```

#### GET /api/readarr/debug
Get Readarr configuration debug information (admin only).

**Query Parameters:**
- `kind` - `ebooks` or `audiobooks` (optional, shows both if not specified)

**Response:**
```json
{
  "ebooks": {
    "base_url": "https://readarr.example.com",
    "api_key": "***redacted***",
    "insecure_skip_verify": false,
    "connected": true,
    "version": "0.1.0.0"
  },
  "audiobooks": {
    "base_url": "https://readarr-audio.example.com",
    "api_key": "***redacted***",
    "insecure_skip_verify": false,
    "connected": true,
    "version": "0.1.0.0"
  }
}
```

**Notes:**
- API keys are redacted for security
- Useful for troubleshooting Readarr connectivity issues

### User Management (Admin Only)

#### GET /users
Returns the user management page (HTML).

**Authentication:** Admin required

#### POST /users
Create a new user.

**Request Body (Form Data):**
- `username` - Username (required)
- `password` - Password (required)
- `is_admin` - Set to "on" for admin privileges

**Response:**
- `302` - Redirect to users page

#### POST /users/edit
Edit an existing user.

**Request Body (Form Data):**
- `user_id` - User ID to edit (required)
- `password` - New password (optional)
- `confirm_password` - Password confirmation (required if password provided)
- `is_admin` - Set to "on" for admin privileges

**Response:**
- `302` - Redirect to users page

#### GET /users/delete
Delete a user.

**Query Parameters:**
- `id` - User ID to delete

**Response:**
- `302` - Redirect to users page

### Notification Test Endpoints (Admin Only)

#### POST /api/notifications/test-ntfy
Test ntfy.sh notification delivery.

**Request Body:**
```json
{
  "server": "https://ntfy.sh",
  "topic": "test-topic",
  "username": "optional-username",
  "password": "optional-password"
}
```

**Response (Success):**
```json
{
  "success": true,
  "message": "Test notification sent successfully"
}
```

**Response (Error):**
```json
{
  "success": false,
  "error": "Failed to send notification: connection timeout"
}
```

#### POST /api/notifications/test-smtp
Test SMTP email delivery.

**Request Body:**
```json
{
  "host": "smtp.gmail.com",
  "port": 587,
  "username": "your-email@gmail.com",
  "password": "your-app-password",
  "from_email": "scriptorum@example.com",
  "from_name": "Scriptorum",
  "to_email": "admin@example.com",
  "enable_tls": true
}
```

**Response (Success):**
```json
{
  "success": true,
  "message": "Test email sent successfully"
}
```

**Response (Error):**
```json
{
  "success": false,
  "error": "SMTP authentication failed"
}
```

#### POST /api/notifications/test-discord
Test Discord webhook delivery.

**Request Body:**
```json
{
  "webhook_url": "https://discord.com/api/webhooks/...",
  "username": "Scriptorum Bot"
}
```

**Response (Success):**
```json
{
  "success": true,
  "message": "Test message sent successfully"
}
```

**Response (Error):**
```json
{
  "success": false,
  "error": "Invalid webhook URL"
}
```

### System Endpoints

#### GET /healthz
Health check endpoint.

**Response:**
```
ok
```

**Notes:**
- Always returns 200 OK if service is running
- Useful for monitoring and load balancer health checks

#### GET /version
Get application version information.

**Response:**
```json
{
  "version": "1.0.0",
  "commit": "abc123",
  "build_time": "2025-01-01T00:00:00Z"
}
```

## Settings Endpoints (Admin Only)

#### GET /settings
Returns the settings management page (HTML).

**Authentication:** Admin required

#### POST /settings/save
Save application settings.

**Request Body (Form Data):**
- `debug` - Enable debug mode ("on"/"off")
- `server_url` - Base server URL
- `ra_ebooks_base` - Readarr ebooks base URL
- `ra_ebooks_key` - Readarr ebooks API key
- `ra_ebooks_insecure` - Skip TLS verification for ebooks ("on"/"off")
- `ra_audiobooks_base` - Readarr audiobooks base URL
- `ra_audiobooks_key` - Readarr audiobooks API key
- `ra_audiobooks_insecure` - Skip TLS verification for audiobooks ("on"/"off")
- And many other configuration options...

**Response:**
- `302` - Redirect to settings page

## UI Endpoints

#### GET /ui/requests/table
Get requests table HTML fragment for HTMX updates.

**Authentication:** Required

**Response:** HTML fragment

#### GET /ui/search
Returns the search interface page (HTML).

**Authentication:** Required

**Query Parameters:**
- `q` - Search query
- `page` - Page number (default: 1)
- `limit` - Results per page (default: 20, max: 50)

#### GET /ui/readarr-cover
Proxy Readarr cover images.

**Query Parameters:**
- `url` - Cover image URL to proxy

**Response:** Image data

## Approval Token Endpoint

#### GET /approve/{token}
One-click request approval from notification links.

**Path Parameters:**
- `token` - Secure approval token

**Response:**
- `302` - Redirect to dashboard with success/error message

**Notes:**
- Tokens expire after 1 hour
- Can be used without authentication
- Useful for email/discord notification approvals

## HTMX Integration

The web interface uses HTMX for dynamic updates. Many endpoints return HTML fragments instead of JSON when called with HTMX headers:

### HTMX Headers
- `HX-Request: true` - Indicates HTMX request
- `HX-Target: #element-id` - Target element for response
- `HX-Swap: innerHTML` - How to swap content

### HTMX Triggers
Some endpoints emit HTMX events:
- `request:updated` - When a request is modified
- `user:created` - When a user is created
- `system:error` - When an error occurs

## Rate Limiting

Currently, no rate limiting is implemented. Consider implementing reverse proxy rate limiting in production.

## CORS

CORS is not explicitly configured. Cross-origin requests may be blocked by browsers.

## WebSocket Support

Scriptorum does not currently support WebSocket connections. All updates are handled via HTMX polling or user-initiated requests.

## Examples

### Create a Request (cURL)
```bash
curl -X POST http://localhost:8080/api/v1/requests \
  -H "Content-Type: application/json" \
  -b "scriptorum_session=your-session-cookie" \
  -d '{
    "title": "The Great Gatsby",
    "authors": ["F. Scott Fitzgerald"],
    "kind": "ebook",
    "selection": {
      "title": "The Great Gatsby",
      "author": "F. Scott Fitzgerald",
      "isbn": "9780743273565",
      "asin": "B004EHZDE8"
    }
  }'
```

### Search Books (cURL)
```bash
curl "http://localhost:8080/api/providers/search?q=great+gatsby&kind=ebooks" \
  -b "scriptorum_session=your-session-cookie"
```

### Approve Request (cURL)
```bash
curl -X POST http://localhost:8080/api/v1/requests/123/approve \
  -H "Content-Type: application/json" \
  -b "scriptorum_session=your-session-cookie"
```

### Get Book Details (cURL)
```bash
curl -X POST http://localhost:8080/api/v1/book/details \
  -H "Content-Type: application/json" \
  -b "scriptorum_session=your-session-cookie" \
  -d '{
    "isbn13": "9780743273565",
    "title": "The Great Gatsby",
    "authors": ["F. Scott Fitzgerald"]
  }'
```

### Test Notification (cURL)
```bash
curl -X POST http://localhost:8080/api/notifications/test-ntfy \
  -H "Content-Type: application/json" \
  -b "scriptorum_session=your-session-cookie" \
  -d '{
    "server": "https://ntfy.sh",
    "topic": "scriptorum-test",
    "username": "",
    "password": ""
  }'
```

### Health Check (cURL)
```bash
curl http://localhost:8080/healthz
# Returns: ok
```

### Get Version (cURL)
```bash
curl http://localhost:8080/version
# Returns: {"version":"1.0.0","commit":"abc123","build_time":"2025-01-01T00:00:00Z"}
```

## JavaScript/Frontend Integration

### Fetch API Example
```javascript
// Search for books
async function searchBooks(query) {
  const response = await fetch(`/api/providers/search?q=${encodeURIComponent(query)}`);
  return await response.json();
}

// Create request
async function createRequest(requestData) {
  const response = await fetch('/api/v1/requests', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(requestData)
  });
  return await response.json();
}

// Get user requests
async function getUserRequests() {
  const response = await fetch('/api/v1/requests');
  return await response.json();
}
```

### HTMX Integration
```html
<!-- Auto-updating request table -->
<div id="request-table" 
     hx-get="/ui/requests/table" 
     hx-trigger="every 30s">
  <!-- Table content loaded here -->
</div>

<!-- Request approval button -->
<button hx-post="/api/v1/requests/123/approve"
        hx-target="closest tr"
        hx-swap="outerHTML">
  Approve
</button>
```

## Error Handling

### Client-Side Error Handling
```javascript
async function handleApiCall(apiFunction) {
  try {
    const result = await apiFunction();
    return result;
  } catch (error) {
    if (error.status === 401) {
      // Redirect to login
      window.location.href = '/login';
    } else if (error.status === 403) {
      // Show access denied message
      alert('Access denied');
    } else {
      // Handle other errors
      console.error('API Error:', error);
    }
  }
}
```

### Server-Side Error Responses
Most endpoints follow this error format:
```json
{
  "error": "Detailed error message",
  "code": "ERROR_CODE",
  "details": {
    "field": "Additional context"
  }
}
```
