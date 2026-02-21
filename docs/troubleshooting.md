# Troubleshooting Guide

This guide documents common failure modes and solutions for operators using `logtap`.

## Table of Contents
- [Receiver Won't Start](#receiver-wont-start)
- [Sidecar Not Forwarding Logs](#sidecar-not-forwarding-logs)
- [`logtap check` Failures](#logtap-check-failures)
- [Disk Full / Rotation Not Working](#disk-full--rotation-not-working)
- [Capture Cannot Be Opened](#capture-cannot-be-opened)
- [Port-Forward Drops / Tunnel Instability](#port-forward-drops--tunnel-instability)
- [Redaction Not Working](#redaction-not-working)
- [`logtap tap` Timed Out](#logtap-tap-timed-out)

---

### Receiver Won't Start

**Symptom**: The `logtap recv` command fails to start or immediately exits with an error.

**Cause**:
- **Port in use**: Another process is already listening on the specified port (`--listen`).
- **Directory permissions**: `logtap` does not have write permissions to the capture directory (`--dir`).
- **Configuration errors**: Invalid values in the config file or command-line flags.

**Solution**:
- **Port in use**:
    - Choose a different port: `logtap recv --listen :9001`
    - Identify and stop the process using the port (e.g., `lsof -i :9000`).
- **Directory permissions**:
    - Ensure the user running `logtap` has write permissions to the `--dir` path.
    - Change the capture directory to an accessible location: `logtap recv --dir /tmp/my-capture`.
- **Configuration errors**:
    - Review the error message for specific configuration issues.
    - Double-check the values provided for flags like `--max-disk`, `--max-file`, etc.
    - If using a config file, ensure it's valid YAML and follows the schema.

### Sidecar Not Forwarding Logs

**Symptom**: `logtap tap` successfully injects the sidecar, but no logs appear in the receiver's TUI or capture directory.

**Cause**:
- **Image pull errors**: The Kubernetes cluster cannot pull the `logtap-forwarder` image (e.g., wrong image name, private registry authentication issues, image not multi-arch compatible with node).
- **Network policy blocking**: A Kubernetes NetworkPolicy prevents the sidecar from reaching the `logtap` receiver.
- **Receiver unreachable**: The `--target` address specified for the sidecar is incorrect or the receiver pod is not running/accessible.
- **Log volume mounting**: The sidecar cannot access the application logs (e.g., incorrect volume mount, application logs to a non-standard location).

**Solution**:
- **Image pull errors**:
    - Verify the sidecar image name and tag: `kubectl describe pod <tapped-pod-name>`.
    - Ensure the image exists and is accessible from the cluster.
    - For private registries, verify image pull secrets are configured correctly.
- **Network policy blocking**:
    - Temporarily disable or adjust relevant NetworkPolicies.
    - Create a NetworkPolicy that allows egress from the sidecar to the receiver.
- **Receiver unreachable**:
    - Double-check the `--target` address is correct (e.g., `logtap.logtap:9000` for in-cluster).
    - Use `kubectl logs <logtap-forwarder-pod>` to check the sidecar logs for connection errors.
    - Verify the receiver is running and its service is reachable (e.g., `kubectl get svc -n logtap logtap`).
- **Log volume mounting**:
    - Inspect the `logtap-forwarder` container spec in the tapped pod to confirm volume mounts.
    - Ensure the application logs to standard output/error or a path that the sidecar can access.

### `logtap check` Failures

**Symptom**: `logtap check` reports warnings or errors regarding RBAC, quota, or orphaned resources.

**Cause**:
- **RBAC Missing**: The Kubernetes user or service account lacks permissions to perform necessary actions (e.g., `patch deployments`, `create pods`).
- **Quota Exceeded**: Injecting sidecars would exceed a namespace's resource quota.
- **Orphaned Resources**: Previous `logtap` sessions were not fully cleaned up, leaving behind sidecars or tunnel pods/services.
- **Prod Namespace Warning**: Attempting to tap a namespace identified as "production" without the `--allow-prod` flag.

**Solution**:
- **RBAC Missing**:
    - Work with a cluster administrator to grant the required RBAC permissions to your user or service account.
    - The `logtap check` output often provides hints about missing permissions.
- **Quota Exceeded**:
    - Increase the namespace's `ResourceQuota` (requires admin privileges).
    - Reduce the sidecar's resource requests using `--sidecar-memory` or `--sidecar-cpu` flags in `logtap tap`.
    - Use `--force` with `logtap tap` if you understand the risks (pods may fail to schedule).
- **Orphaned Resources**:
    - Follow the suggestions from `logtap check` to clean up:
        - `logtap untap --all` to remove orphaned sidecars.
        - `kubectl delete ns logtap` to remove orphaned tunnel resources if `logtap` was deployed in-cluster.
- **Prod Namespace Warning**:
    - If you intend to tap a production namespace, explicitly use the `--allow-prod` flag with `logtap tap`. Be aware of the implications.
    - Ensure PII redaction is enabled (`--redact`) if tapping production.

### Disk Full / Rotation Not Working

**Symptom**: The receiver stops writing logs, reports drops, or the capture directory exceeds its `--max-disk` limit without old files being deleted.

**Cause**:
- **Disk write errors**: The underlying disk is full, has I/O errors, or permissions issues.
- **Incorrect `--max-disk` or `--max-file`**: Values are too high, or a configuration mistake prevents rotation.
- **Slow file deletion**: The system is slow to delete files, or `logtap` is unable to delete files.

**Solution**:
- **Disk write errors**:
    - Check available disk space on the volume hosting the capture directory.
    - Investigate system logs for disk-related errors.
    - Ensure `logtap` has full permissions to manage files in the capture directory.
- **Incorrect limits**:
    - Review `logtap recv` command flags and config file for `--max-disk` and `--max-file` values.
    - Ensure `--max-disk` is a reasonable limit for your storage.
- **Slow file deletion**:
    - Monitor `logtap`'s internal metrics for `logtap_disk_usage_bytes` and `logtap_backpressure_events_total`.
    - Consider increasing disk I/O capacity or reducing log volume if persistently hitting limits.

### Capture Cannot Be Opened

**Symptom**: `logtap open`, `inspect`, `slice`, or `export` fail with errors about corrupt metadata, missing index, or invalid file formats.

**Cause**:
- **Corrupt `metadata.json`**: The `metadata.json` file in the capture directory is malformed or missing.
- **Corrupt `index.jsonl`**: The `index.jsonl` file is malformed, preventing proper indexing of log files.
- **Incomplete capture**: The `logtap recv` process was terminated abruptly without flushing buffers, leaving an incomplete or inconsistent capture.
- **Manual tampering**: Files within the capture directory were manually modified, moved, or deleted.

**Solution**:
- **Corrupt files**:
    - If the capture is critical, attempt to manually repair `metadata.json` or `index.jsonl` if the corruption is minor and understandable.
    - For a robust solution, avoid manual modification of capture directories.
- **Incomplete capture**:
    - Always ensure `logtap recv` is gracefully shut down (Ctrl+C) to allow for buffer flushing and metadata updates.
    - For corrupted captures, try `logtap open --force` (if available and supported for recovery), but data integrity cannot be guaranteed.
- **Manual tampering**:
    - Restore the capture directory from a backup if available.
    - Avoid direct manipulation of files within `logtap` capture directories.

### Port-Forward Drops / Tunnel Instability

**Symptom**: When using `logtap recv --tunnel`, the connection between the in-cluster forwarder and your local receiver frequently disconnects or logs stop flowing.

**Cause**:
- **Local network instability**: Your machine's network connection is unreliable.
- **Kubernetes API server load**: The Kubernetes API server is under heavy load, causing `kubectl port-forward` to become unstable.
- **Receiver pod restart**: The temporary receiver pod in the cluster (used for the tunnel) is restarting due to resource limits, errors, or Kubernetes eviction policies.
- **Idle timeouts**: Some network infrastructure or VPNs may aggressively close idle connections, even if traffic is low.

**Solution**:
- **Local network stability**:
    - Ensure your local machine has a stable network connection.
    - Avoid actions that might disrupt network connectivity (e.g., VPN changes).
- **Kubernetes API server load**:
    - Monitor the Kubernetes API server's health and resource usage.
    - If API server load is an issue, consider deploying `logtap recv --in-cluster` for more stability.
- **Receiver pod restart**:
    - Use `kubectl describe pod <receiver-pod-name> -n logtap` to check for restart reasons (OOMKilled, errors).
    - Increase the resource requests/limits for the in-cluster receiver pod if necessary.
- **Idle timeouts**:
    - If possible, configure your network or VPN to have longer idle timeouts.
    - Consider `logtap recv --in-cluster` as an alternative if the tunnel remains unstable.

### Redaction Not Working

**Symptom**: PII or sensitive data is still visible in captured logs despite using the `--redact` flag.

**Cause**:
- **Incorrect flag usage**: `--redact` was not used, or specific patterns were not enabled (e.g., `--redact=email` but a credit card number is visible).
- **Pattern not matching**: The built-in or custom redaction patterns do not correctly match the format of the sensitive data in the logs.
- **Custom patterns file format**: The `--redact-patterns` YAML file is malformed or its regex patterns are incorrect.
- **Redaction pipeline bypass**: Logs are entering the system through a path that bypasses the redaction pipeline.

**Solution**:
- **Correct flag usage**:
    - Ensure `logtap recv --redact` is used to enable all built-in patterns.
    - If specific patterns are desired, ensure all relevant ones are listed (e.g., `--redact=credit_card,email,jwt`).
- **Pattern not matching**:
    - Examine the sensitive data's exact format in the raw logs.
    - If using custom patterns, test the regex patterns against sample data using an online regex tester.
    - Built-in patterns are comprehensive but might not cover highly unusual formats.
- **Custom patterns file format**:
    - Validate your `patterns.yaml` file using a YAML linter.
    - Ensure the `regex` values are valid regular expressions.
- **Redaction pipeline bypass**:
    - Confirm all log ingestion paths (Loki push API, raw JSON) are properly configured to pass through the redaction stage. This is usually handled internally by `logtap` when `--redact` is active.

### `logtap tap` Timed Out

**Symptom**: `logtap tap` command returns a timeout error after a period of waiting, especially when interacting with Kubernetes.

**Cause**:
- **Unresponsive Kubernetes API server**: The Kubernetes API server is slow, overloaded, or unreachable from where `logtap` is running.
- **Network latency**: High network latency between the `logtap` CLI and the Kubernetes cluster.
- **Resource constraints on `logtap` host**: The machine running `logtap` is under heavy load, causing operations to be slow.
- **Long-running cluster operations**: The Kubernetes cluster itself is busy with other operations (e.g., many rolling updates) that delay `logtap`'s ability to patch resources.

**Solution**:
- **Unresponsive Kubernetes API server**:
    - Check the health and load of your Kubernetes API server (`kubectl top node`, `kubectl get --raw=/metrics`).
    - If possible, try running `logtap` from a location with better network connectivity to the cluster.
- **Increase timeout**:
    - Use the `--timeout` flag to allow `logtap` more time to complete its operations, e.g., `logtap tap --deployment my-app --target ... --timeout 60s`.
    - This is a workaround; address the root cause of the slow API server if possible.
- **Check local machine resources**:
    - Ensure the machine running the `logtap` CLI has sufficient CPU and memory.
- **Monitor cluster activity**:
    - Observe if other cluster operations are ongoing that might be consuming resources or locking resources `logtap` needs to modify.
    - Retry the `logtap tap` command after some time.