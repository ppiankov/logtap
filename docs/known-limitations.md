# Known Limitations

## General

- **Capture format** — capture directory structure is stable; compression codec may evolve
- **Sidecar resource overhead** — each sidecar adds 16Mi/25m requests by default
- **No browser UI** — TUI only, terminal required
- **No CRDs or operators** — imperative CLI workflow
- **No long-term retention** — bounded disk, oldest files deleted automatically

## Pod restart on tap

Sidecar injection triggers a pod restart. This is inherent to how Kubernetes handles container spec changes. Use `--dry-run` to preview before applying.

## Scanning a live capture

`logtap triage`, `grep`, `slice`, and `export` can safely run against a capture directory that is still receiving logs. File rotation may delete old data files during a long-running scan — these are skipped gracefully. Triage additionally performs a catch-up pass after the main scan to pick up files that were created by rotation during the initial scan. Line counts may differ slightly from the final capture since rotation is concurrent.

## Image availability on tap

`logtap tap` injects a forwarder sidecar into workloads, which triggers a pod restart. During restart, Kubernetes pulls both the application image and the forwarder image (`ghcr.io/ppiankov/logtap-forwarder`). If either image is unavailable — registry down, credentials expired, image deleted, air-gapped cluster — the pod will enter `ImagePullBackOff` and fail to start.

**Before tapping in production-adjacent environments:**

1. Use `--dry-run` first to preview changes without applying
2. Verify the forwarder image is pullable: `kubectl run --rm -it --image ghcr.io/ppiankov/logtap-forwarder:latest --restart=Never test -- /bin/true`
3. Verify the application image is still in your registry — long-running pods may reference images that have since been garbage-collected

If your cluster has image availability risks (stale registries, harbor cleanup policies, air-gapped nodes), consider running [tote](https://github.com/ppiankov/tote) — an emergency operator that detects `ImagePullBackOff` and salvages cached images from other nodes via node-to-node transfer.
