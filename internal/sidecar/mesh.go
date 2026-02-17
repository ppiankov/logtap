package sidecar

import (
	"net"
	"strings"
)

const (
	// Linkerd annotations
	linkerdInjectAnnotation = "linkerd.io/inject"
	linkerdSkipOutbound     = "config.linkerd.io/skip-outbound-ports"

	// Istio annotations
	istioInjectAnnotation = "sidecar.istio.io/inject"
	istioRevAnnotation    = "istio.io/rev"
	istioExcludeOutbound  = "traffic.sidecar.istio.io/excludeOutboundPorts"
)

// MeshBypassAnnotations returns annotations to bypass service mesh proxying
// for the given target port. It detects Linkerd and Istio from existing
// pod template annotations and returns the appropriate bypass annotations.
// Returns an empty map if no mesh is detected.
func MeshBypassAnnotations(existing map[string]string, targetPort string) map[string]string {
	if targetPort == "" {
		return nil
	}

	result := make(map[string]string)

	// Linkerd detection
	if existing[linkerdInjectAnnotation] == "enabled" {
		result[linkerdSkipOutbound] = mergePort(existing[linkerdSkipOutbound], targetPort)
	}

	// Istio detection
	if existing[istioInjectAnnotation] == "true" || hasAnnotationPrefix(existing, istioRevAnnotation) {
		result[istioExcludeOutbound] = mergePort(existing[istioExcludeOutbound], targetPort)
	}

	return result
}

// MeshBypassAnnotationKeys returns the annotation keys that MeshBypassAnnotations
// may have added, for cleanup during untap.
func MeshBypassAnnotationKeys() []string {
	return []string{linkerdSkipOutbound, istioExcludeOutbound}
}

// extractPort returns the port from a host:port target string.
func extractPort(target string) string {
	_, port, err := net.SplitHostPort(target)
	if err != nil {
		return ""
	}
	return port
}

// mergePort adds port to a comma-separated port list if not already present.
func mergePort(existing, port string) string {
	if existing == "" {
		return port
	}
	for _, p := range strings.Split(existing, ",") {
		if strings.TrimSpace(p) == port {
			return existing
		}
	}
	return existing + "," + port
}

// hasAnnotationPrefix checks if any annotation key matches the given key exactly.
func hasAnnotationPrefix(annotations map[string]string, key string) bool {
	_, ok := annotations[key]
	return ok
}
