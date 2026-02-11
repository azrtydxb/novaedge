# WAF (Web Application Firewall)

NovaEdge includes an integrated Web Application Firewall powered by the Coraza WAF engine,
providing protection against common web attacks including SQL injection, XSS, and more.

## Overview

The WAF inspects incoming HTTP requests against a set of security rules and can either
block malicious requests (prevention mode) or log them without blocking (detection mode).

## Configuration

### Kubernetes CRD (ProxyPolicy)

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: waf-policy
spec:
  type: WAF
  targetRef:
    kind: ProxyRoute
    name: api-route
  waf:
    enabled: true
    mode: prevention
    paranoiaLevel: 1
    anomalyThreshold: 5
    ruleExclusions:
      - "920350"  # Exclude specific rules
```

### Standalone Mode

```yaml
policies:
  - name: waf-policy
    type: WAF
    waf:
      enabled: true
      mode: prevention
      paranoiaLevel: 2
      anomalyThreshold: 10
      customRules:
        - 'SecRule REQUEST_HEADERS:X-Custom "@rx malicious" "id:100001,phase:1,deny,status:403"'
```

## Configuration Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | true | Enable/disable WAF |
| `mode` | string | `prevention` | `detection` (log only) or `prevention` (block) |
| `paranoiaLevel` | int | 1 | OWASP CRS paranoia level (1-4) |
| `anomalyThreshold` | int | 5 | Anomaly score threshold for blocking |
| `rulesConfigMap` | ref | - | ConfigMap containing custom WAF rules |
| `ruleExclusions` | string[] | - | Rule IDs to exclude |

## Paranoia Levels

| Level | Description |
|-------|-------------|
| 1 | Standard protection - minimal false positives |
| 2 | Elevated protection - some additional rules |
| 3 | High protection - stricter rules |
| 4 | Maximum protection - may have false positives |

## Operating Modes

### Prevention Mode

Malicious requests are blocked and a 403 Forbidden response is returned.

### Detection Mode

Malicious requests are logged but allowed through. Use this mode when first deploying
WAF to identify false positives before switching to prevention mode.

## Built-in Rules

NovaEdge includes built-in rules for:

- **SQL Injection** (Rule ID 1001): Detects common SQL injection patterns
- **Cross-Site Scripting (XSS)** (Rule ID 1002): Detects script injection attempts
- **Path Traversal** (Rule ID 1003): Detects directory traversal attempts
- **Command Injection** (Rule ID 1004): Detects OS command injection attempts

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `novaedge_waf_requests_blocked_total` | Counter | Requests blocked by WAF |
| `novaedge_waf_rules_matched_total` | Counter | Total WAF rules matched |
| `novaedge_waf_anomaly_score` | Histogram | Anomaly score distribution |
