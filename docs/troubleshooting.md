# Troubleshooting

Common failure modes and solutions for logtap operators.

---

## Receiver won't start

**Symptom:** `logtap recv` exits immediately with "address already in use".

**Cause:** Another process (or a previous logtap instance) is bound to the port.

**Solution:**
```bash
# Find what's using the port
lsof -i :9000

# Use a different port
logtap recv --port 9001
```

---

## Receiver fills disk

**Symptom:** `logtap recv` stops writing, logs show "disk cap reached" or the output directory is full.

**Cause:** The `--cap` limit was reached or the filesystem is full. Rotation drops oldest files but cannot free space below OS-level limits.

**Solution:**
```bash
# Increase the cap
logtap recv --cap 10GB

# Clean old captures
rm -rf /path/to/old-captures/

# Check disk space
df -h .
```

---

## Sidecar not forwarding logs

**Symptom:** Sidecar container is running but receiver gets no data.

**Cause:** Network policy blocking traffic, incorrect receiver target URL, or image pull failure.

**Solution:**
```bash
# Check sidecar logs
kubectl logs <pod> -c logtap-forwarder-<session-id>

# Verify the receiver is reachable from the cluster
kubectl run curl-test --rm -it --image=curlimages/curl -- curl -s http://<receiver-service>:9000/healthz

# Check for image pull errors
kubectl describe pod <pod> | grep -A5 "Events:"
```

---

## `logtap check` reports RBAC errors

**Symptom:** `logtap check` shows "FAIL" for RBAC permissions.

**Cause:** The current kubeconfig context doesn't have permission to patch Deployments/StatefulSets/DaemonSets.

**Solution:**
```bash
# Check what permissions you have
kubectl auth can-i patch deployments
kubectl auth can-i patch daemonsets

# If using a service account, bind the required ClusterRole:
kubectl create clusterrolebinding logtap-admin \
  --clusterrole=edit \
  --serviceaccount=default:default
```

---

## Capture can't be opened

**Symptom:** `logtap open` or `logtap inspect` reports "invalid capture directory" or missing metadata.

**Cause:** The `metadata.json` or `index.jsonl` file is missing or corrupt. This can happen if the receiver was killed before finalizing.

**Solution:**
```bash
# Check what exists
ls -la <capture-dir>/

# If metadata.json is missing, the capture is unrecoverable via CLI.
# Data files (.jsonl, .jsonl.zst) can still be read manually:
zstd -d <capture-dir>/*.jsonl.zst
cat <capture-dir>/*.jsonl | jq .
```

---

## Port-forward drops frequently

**Symptom:** `logtap recv --in-cluster` port-forward dies after a few minutes, logs stop flowing.

**Cause:** Kubernetes port-forward is inherently unstable — idle connections are dropped by load balancers, NAT timeouts, or API server restarts.

**Solution:**
```bash
# Run with explicit timeout extension (if your kubectl supports it)
kubectl port-forward pod/logtap-receiver 9000:9000 --pod-running-timeout=0

# Alternative: use a NodePort or LoadBalancer service instead of port-forward
# for long-running captures
```

---

## Redaction not matching expected patterns

**Symptom:** `logtap recv --redact` is enabled but sensitive data still appears in captures.

**Cause:** The default patterns cover common formats (emails, IPs, JWTs). Custom patterns may not match your data format.

**Solution:**
```bash
# Check what patterns are active
logtap recv --redact --redact-show-patterns

# Add custom patterns via config file (~/.config/logtap/config.yaml)
# redact_patterns:
#   - "SSN:\\s*\\d{3}-\\d{2}-\\d{4}"
#   - "api_key=[A-Za-z0-9]{32}"

# Test a pattern against sample data
echo 'user email: test@example.com' | grep -oP 'your-pattern-here'
```

---

## Fluent Bit sidecar fails to start

**Symptom:** Fluent Bit sidecar container enters CrashLoopBackOff after `logtap tap --forwarder fluent-bit`.

**Cause:** ConfigMap not created, incorrect image, or volume mount permissions.

**Solution:**
```bash
# Check ConfigMap exists
kubectl get configmap logtap-fb-<session-id>

# Check sidecar logs
kubectl logs <pod> -c logtap-forwarder-<session-id>

# Verify the image is pullable
kubectl run fb-test --rm -it --image=fluent/fluent-bit:3.0 -- /fluent-bit/bin/fluent-bit --version

# Remove and retry
logtap untap <workload>
logtap tap <workload> --forwarder fluent-bit --image fluent/fluent-bit:3.0
```

---

## Triage shows no error patterns

**Symptom:** `logtap triage` returns empty results even though logs contain errors.

**Cause:** Error detection uses keyword matching (error, fatal, panic, exception, fail). If your error logs use different patterns, they won't be detected.

**Solution:**
```bash
# Check what the triage scans for
logtap triage <capture-dir> --json | jq .

# Use grep for custom error patterns
logtap grep "YOUR_ERROR_PATTERN" <capture-dir>

# Check if entries exist at all
logtap inspect <capture-dir>
```
