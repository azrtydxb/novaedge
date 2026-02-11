# Custom Error Pages

NovaEdge supports custom error pages that replace default HTTP error responses
(4xx and 5xx) with branded, user-friendly HTML pages.

## Overview

When a backend returns an error response, or the proxy itself generates one
(e.g., no route found, backend unavailable), NovaEdge can intercept the response
and serve a custom HTML page instead.

## Configuration

### Kubernetes CRD

Custom error pages are configured in the `ProxyGateway` spec:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: web-gateway
spec:
  vipRef: web-vip
  listeners:
    - name: http
      port: 80
      protocol: HTTP
  customErrorPages:
    - codes: [404]
      body: |
        <!DOCTYPE html>
        <html>
        <body>
          <h1>Page Not Found</h1>
          <p>The page you requested could not be found.</p>
          <p>Request ID: {{.RequestID}}</p>
        </body>
        </html>
      contentType: "text/html"
    - codes: [500, 502, 503]
      body: |
        <!DOCTYPE html>
        <html>
        <body>
          <h1>{{.StatusCode}} - {{.StatusText}}</h1>
          <p>We're experiencing technical difficulties. Please try again later.</p>
          <p>Time: {{.Timestamp}}</p>
        </body>
        </html>
      contentType: "text/html"
```

### Standalone Mode

In `novaedge.yaml`:

```yaml
errorPages:
  enabled: true
  pages:
    404: |
      <html><body><h1>Not Found</h1><p>Request ID: {{.RequestID}}</p></body></html>
    503: |
      <html><body><h1>Service Unavailable</h1></body></html>
  defaultPage: |
    <html><body><h1>{{.StatusCode}} {{.StatusText}}</h1></body></html>
```

## Template Variables

Error page templates support the following variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.StatusCode}}` | HTTP status code | `404` |
| `{{.StatusText}}` | HTTP status text | `Not Found` |
| `{{.RequestID}}` | Value of X-Request-ID header | `abc-123-def` |
| `{{.Timestamp}}` | UTC timestamp in RFC3339 format | `2024-01-15T10:30:00Z` |

## Behavior

- Error pages are only served for **4xx and 5xx** status codes
- **2xx and 3xx** responses pass through unchanged
- If no custom page is configured for a specific status code, the **default page** is used
- If no default page is configured, a **built-in default** page is rendered
- The original backend response body is **discarded** when a custom page is served
- The `Content-Type` header is set to `text/html; charset=utf-8`

## Built-in Default Page

When no custom or default page is configured, NovaEdge serves a clean, minimal
HTML page showing the status code, status text, request ID, and timestamp.
