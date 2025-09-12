# API Documentation

This document describes the Scriptorum REST API endpoints and their usage.

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
https://your-scriptorum-instance.com/api/v1/
```

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
