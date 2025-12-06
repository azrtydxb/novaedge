# Troubleshooting

Common issues and solutions for NovaEdge deployments.

## Quick Diagnostics

### System Status

```bash
# Check all pods
kubectl get pods -n novaedge-system

# Check cluster status (operator mode)
kubectl get novaedgecluster -n novaedge-system

# Check CRDs
kubectl get crds | grep novaedge

# Using novactl
novactl status
```

### Component Logs

```bash
# Controller logs
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-controller

# Agent logs
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent

# Operator logs
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-operator
```

## Installation Issues

### CRDs Not Found

**Symptom**: `error: the server doesn't have a resource type "proxygateways"`

**Solution**:
```bash
# Reinstall CRDs
make install-crds

# Or manually
kubectl apply -f config/crd/

# Verify
kubectl get crds | grep novaedge.io
```

### Controller Not Starting

**Symptom**: Controller pod in CrashLoopBackOff

**Diagnosis**:
```bash
# Check pod events
kubectl describe pod -n novaedge-system -l app.kubernetes.io/name=novaedge-controller

# Check logs
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-controller --previous
```

**Common Causes**:

| Cause | Solution |
|-------|----------|
| RBAC issues | Check ServiceAccount and ClusterRole |
| Missing CRDs | Install CRDs first |
| Resource limits | Increase memory/CPU limits |
| Leader election | Check lease object in namespace |

### Agents Not Connecting

**Symptom**: Agents not receiving configuration

**Diagnosis**:
```bash
# Check agent logs
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent | grep -i "connect\|error"

# Test connectivity
kubectl exec -n novaedge-system <agent-pod> -- nc -zv novaedge-controller 9090

# Check controller service
kubectl get svc -n novaedge-system novaedge-controller
```

**Solutions**:
```bash
# Verify controller address
kubectl get deployment novaedge-agent -n novaedge-system -o yaml | grep CONTROLLER_ADDR

# Check network policies
kubectl get networkpolicies -n novaedge-system
```

## Routing Issues

### Route Not Matching

**Symptom**: 404 for requests that should match

**Diagnosis**:
```bash
# Check route configuration
kubectl get proxyroute <route-name> -o yaml

# Check gateway listeners
kubectl get proxygateway <gateway-name> -o yaml

# Test with curl
curl -v -H "Host: example.com" http://<vip>/path
```

**Common Causes**:

| Cause | Check |
|-------|-------|
| Hostname mismatch | Verify `hostnames` in route |
| Path mismatch | Check `path.type` (Exact vs PathPrefix) |
| Gateway not listening | Verify listener port and protocol |
| Route not attached | Check `parentRefs` in route |

### Backend Not Receiving Traffic

**Symptom**: Route matches but backend returns errors

**Diagnosis**:
```bash
# Check backend status
kubectl get proxybackend <backend-name> -o yaml

# Check endpoints
kubectl get endpoints <service-name>

# Test backend directly
kubectl exec -n novaedge-system <agent-pod> -- curl -v http://<backend-ip>:<port>/health
```

## VIP Issues

### VIP Not Reachable

**Symptom**: Cannot reach VIP address

**Diagnosis**:
```bash
# Check VIP status
kubectl get proxyvip -o yaml

# Check agent logs for VIP
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent | grep -i vip

# Verify VIP is bound (on active node)
ip addr show | grep <vip-address>
```

**Solutions by Mode**:

#### L2 Mode
```bash
# Check ARP
arp -a | grep <vip-address>

# Send test ARP
arping -I eth0 <vip-address>

# Verify interface
kubectl get proxyvip -o yaml | grep interface
```

#### BGP Mode
```bash
# Check BGP sessions
kubectl exec -n novaedge-system <agent-pod> -- novactl bgp status

# Verify peer config
kubectl get proxyvip -o yaml | grep -A10 bgp

# Check router
show ip bgp summary
show ip route <vip-address>
```

### VIP Failover Not Working

**Symptom**: VIP stuck on failed node

**Diagnosis**:
```bash
# Check controller logs for failover
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-controller | grep -i failover

# Check VIP assignment
kubectl get proxyvip -o yaml | grep currentNode

# Verify health checks
kubectl describe proxyvip <vip-name>
```

## TLS Issues

### Certificate Not Found

**Symptom**: TLS handshake fails

**Diagnosis**:
```bash
# Check secret exists
kubectl get secret <tls-secret-name>

# Verify certificate data
kubectl get secret <tls-secret-name> -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text

# Check gateway TLS config
kubectl get proxygateway -o yaml | grep -A10 tls
```

### Certificate Expired

**Symptom**: TLS errors in browser/client

**Solution**:
```bash
# Check expiry
kubectl get secret <tls-secret-name> -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -dates

# Renew with cert-manager
kubectl delete certificate <cert-name>
kubectl apply -f certificate.yaml
```

### SNI Not Matching

**Symptom**: Wrong certificate served

**Solution**:
- Verify hostname in certificate matches request
- Check listener hostnames configuration
- Ensure correct certificate order in `certificateRefs`

## Policy Issues

### Rate Limiting Not Working

**Symptom**: Rate limits not enforced

**Diagnosis**:
```bash
# Check policy attached
kubectl get proxypolicy -o yaml | grep targetRef

# Check policy config
kubectl describe proxypolicy <policy-name>

# Test rate limit
for i in {1..150}; do curl -s http://<vip>/api; done
```

### JWT Validation Failing

**Symptom**: 401 for valid tokens

**Diagnosis**:
```bash
# Verify JWKS endpoint
curl -v <jwks-uri>

# Check issuer match
kubectl get proxypolicy -o yaml | grep issuer

# Decode token
echo <token> | cut -d. -f2 | base64 -d | jq
```

**Common Causes**:

| Cause | Solution |
|-------|----------|
| Wrong issuer | Match `iss` claim to policy |
| Wrong audience | Match `aud` claim to policy |
| JWKS unreachable | Check network from agent |
| Clock skew | Sync NTP on nodes |

## Performance Issues

### High Latency

**Diagnosis**:
```bash
# Check metrics
curl http://localhost:9090/metrics | grep request_duration

# Check backend health
kubectl get proxybackend -o yaml | grep -A10 healthCheck

# Check connection pool
curl http://localhost:9090/metrics | grep connections
```

**Solutions**:
- Increase connection pool size
- Use latency-aware LB (EWMA)
- Enable keep-alive
- Check backend capacity

### Connection Timeouts

**Diagnosis**:
```bash
# Check timeout configuration
kubectl get proxybackend -o yaml | grep timeout

# Check circuit breaker
curl http://localhost:9090/metrics | grep circuit_breaker
```

**Solutions**:
```yaml
# Increase timeouts
spec:
  timeout:
    connect: 10s
    request: 60s
    idle: 120s
```

### Memory/CPU Issues

**Diagnosis**:
```bash
# Check resource usage
kubectl top pods -n novaedge-system

# Check OOM kills
kubectl describe pod <pod-name> | grep -i oom
```

**Solutions**:
```yaml
# Increase resources
resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 2000m
    memory: 2Gi
```

## Health Check Issues

### All Backends Unhealthy

**Symptom**: No healthy endpoints

**Diagnosis**:
```bash
# Check health check config
kubectl get proxybackend -o yaml | grep -A10 healthCheck

# Test health endpoint manually
kubectl exec -n novaedge-system <agent-pod> -- curl -v http://<backend>:<port>/health

# Check agent logs
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent | grep health
```

**Common Causes**:

| Cause | Solution |
|-------|----------|
| Wrong path | Verify healthCheck.path |
| Wrong port | Check service port mapping |
| Timeout too short | Increase healthCheck.timeout |
| Endpoint unreachable | Check network policies |

## Multi-Cluster Issues

### Remote Cluster Not Connecting

**Diagnosis**:
```bash
# Check remote cluster status
kubectl get novaedgeremotecluster -n novaedge-system

# Check agent logs on spoke
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent | grep connect

# Verify mTLS certificates
kubectl get secret novaedge-agent-cert -n novaedge-system
```

**Solutions**:
- Verify controller endpoint is reachable
- Check firewall rules for gRPC port
- Validate mTLS certificates
- Check certificate expiry

## Getting Help

### Collect Debug Information

```bash
# Generate debug bundle
novactl debug bundle -o novaedge-debug.tar.gz

# Or manually
kubectl get all -n novaedge-system -o yaml > novaedge-resources.yaml
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-controller > controller.log
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent > agent.log
```

### Check Documentation

- [Architecture](../architecture/overview.md)
- [CRD Reference](../reference/crd-reference.md)
- [GitHub Issues](https://github.com/piwi3910/novaedge/issues)

### Community Support

- GitHub Issues: Report bugs and request features
- Discussions: Ask questions and share solutions
