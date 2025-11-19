# API Documentation

## Authentication

All protected endpoints require a JWT token in the `Authorization` header:

```http
Authorization: Bearer <your-jwt-token>
```

### Token Structure

```json
{
  "sub": "user-id",
  "email": "user@example.com",
  "iat": 1234567890,
  "exp": 1234567890
}
```

## Core Service API

### Base URL
`https://api.mikeodnis.dev/v1/core`

### Endpoints

#### List Users
```http
GET /users
Authorization: Bearer <token>
```

**Response:**
```json
{
  "users": [
    {
      "id": "uuid",
      "email": "user@example.com",
      "name": "John Doe",
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

#### Create User
```http
POST /users
Authorization: Bearer <token>
Content-Type: application/json

{
  "email": "user@example.com",
  "name": "John Doe"
}
```

#### Update User
```http
PATCH /users/{id}
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Jane Doe"
}
```

## Media Service API

### Base URL
`https://api.mikeodnis.dev/v1/media`

### Endpoints

#### Upload File
```http
POST /upload
Authorization: Bearer <token>
Content-Type: multipart/form-data

file: <binary>
```

**Response:**
```json
{
  "id": "file-hash",
  "filename": "image.jpg",
  "url": "https://cdn.mikeodnis.dev/uploads/user-id/file-hash.jpg",
  "size": 1024000,
  "mime_type": "image/jpeg",
  "created_at": "2024-01-01T00:00:00Z"
}
```

#### Generate Pre-signed URL
```http
POST /presign
Authorization: Bearer <token>
Content-Type: application/json

{
  "filename": "image.jpg"
}
```

**Response:**
```json
{
  "url": "https://...presigned-url...",
  "key": "uploads/user-id/timestamp.jpg",
  "expires_at": "2024-01-01T00:15:00Z"
}
```

**Usage:**
```bash
# Upload file using pre-signed URL
curl -X PUT "<presigned-url>" \
  --upload-file image.jpg \
  -H "Content-Type: image/jpeg"
```

#### List Assets
```http
GET /assets
Authorization: Bearer <token>
```

**Response:**
```json
{
  "assets": [
    {
      "key": "uploads/user-id/hash.jpg",
      "size": 1024000,
      "last_modified": "2024-01-01T00:00:00Z",
      "url": "https://cdn.mikeodnis.dev/uploads/user-id/hash.jpg"
    }
  ]
}
```

#### Delete Asset
```http
DELETE /assets/{id}
Authorization: Bearer <token>
```

## Links Service (GraphQL)

### Base URL
`https://api.mikeodnis.dev/v1/links/graphql`

### Queries

#### Get Link by Slug
```graphql
query GetLinkBySlug($slug: String!) {
  links_links(where: {slug: {_eq: $slug}, is_active: {_eq: true}}) {
    id
    slug
    target_url
    title
    description
    clicks
  }
}
```

**Variables:**
```json
{
  "slug": "my-link"
}
```

#### List User Links
```graphql
query ListUserLinks {
  links_links(order_by: {created_at: desc}) {
    id
    slug
    target_url
    title
    description
    clicks
    is_active
    created_at
    updated_at
  }
}
```

### Mutations

#### Create Link
```graphql
mutation CreateLink($slug: String!, $target_url: String!, $title: String, $description: String) {
  insert_links_links_one(object: {
    slug: $slug,
    target_url: $target_url,
    title: $title,
    description: $description
  }) {
    id
    slug
    target_url
    title
    description
    created_at
  }
}
```

**Variables:**
```json
{
  "slug": "my-link",
  "target_url": "https://example.com",
  "title": "My Link",
  "description": "A cool link"
}
```

#### Update Link
```graphql
mutation UpdateLink($id: uuid!, $target_url: String, $title: String, $description: String, $is_active: Boolean) {
  update_links_links_by_pk(
    pk_columns: {id: $id},
    _set: {
      target_url: $target_url,
      title: $title,
      description: $description,
      is_active: $is_active
    }
  ) {
    id
    slug
    target_url
    is_active
    updated_at
  }
}
```

#### Get Link Analytics
```graphql
query GetLinkAnalytics($link_id: uuid!) {
  links_clicks(where: {link_id: {_eq: $link_id}}, order_by: {clicked_at: desc}, limit: 100) {
    id
    clicked_at
    country
    referer
    user_agent
  }
  links_clicks_aggregate(where: {link_id: {_eq: $link_id}}) {
    aggregate {
      count
    }
  }
}
```

## Rate Limits

| Service | Authenticated | Anonymous |
|---------|---------------|-----------|
| Core    | 100 req/min   | N/A       |
| Media   | 50 req/min    | N/A       |
| Links   | 100 req/min   | 10 req/min |

**Headers:**
```http
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1234567890
```

## Error Responses

All errors follow this format:

```json
{
  "error": "Error message",
  "code": "ERROR_CODE",
  "details": {}
}
```

### Common HTTP Status Codes

- `200 OK` - Success
- `201 Created` - Resource created
- `204 No Content` - Success with no response body
- `400 Bad Request` - Invalid request
- `401 Unauthorized` - Missing or invalid token
- `403 Forbidden` - Insufficient permissions
- `404 Not Found` - Resource not found
- `429 Too Many Requests` - Rate limit exceeded
- `500 Internal Server Error` - Server error
- `503 Service Unavailable` - Service temporarily unavailable

## CORS

Allowed origins:
- `https://mikeodnis.dev`
- `https://www.mikeodnis.dev`
- `http://localhost:*` (development only)

## Versioning

API uses URL path versioning (`/v1/`). Breaking changes will result in a new version (`/v2/`).

Deprecated endpoints include a `Sunset` header:

```http
Sunset: Sat, 31 Dec 2024 23:59:59 GMT
Deprecation: true
```

## Client Libraries

### TypeScript/JavaScript

```typescript
// Generate from OpenAPI
npm install openapi-typescript-codegen

npx openapi-typescript-codegen \
  --input https://api.mikeodnis.dev/v1/core/openapi.json \
  --output ./src/generated/core-api
```

### GraphQL (Links Service)

```bash
# Generate types
npm install @graphql-codegen/cli
npx graphql-codegen init
```

## Examples

### Full Upload Flow

```typescript
// 1. Get pre-signed URL
const presignResponse = await fetch('https://api.mikeodnis.dev/v1/media/presign', {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({ filename: 'photo.jpg' })
});

const { url, key } = await presignResponse.json();

// 2. Upload file directly to R2
await fetch(url, {
  method: 'PUT',
  body: fileBlob,
  headers: {
    'Content-Type': 'image/jpeg'
  }
});

// 3. File is now available at:
// https://cdn.mikeodnis.dev/${key}
```

### Link Shortener Flow

```typescript
// 1. Create short link
const mutation = `
  mutation CreateLink($slug: String!, $target_url: String!) {
    insert_links_links_one(object: {
      slug: $slug,
      target_url: $target_url
    }) {
      id
      slug
    }
  }
`;

const response = await fetch('https://api.mikeodnis.dev/v1/links/graphql', {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    query: mutation,
    variables: {
      slug: 'my-link',
      target_url: 'https://example.com'
    }
  })
});

// 2. Link available at: https://mikeodnis.dev/my-link
```

## Support

- OpenAPI Documentation: https://api.mikeodnis.dev/v1/core/docs
- GraphQL Playground: https://api.mikeodnis.dev/v1/links/graphql
- Status Page: https://status.mikeodnis.dev
