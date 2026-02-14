package sidecar

import (
	"testing"
)

func TestContainerName(t *testing.T) {
	cfg := SidecarConfig{SessionID: "lt-a3f9"}
	if got := cfg.ContainerName(); got != "logtap-forwarder-lt-a3f9" {
		t.Errorf("ContainerName() = %q, want %q", got, "logtap-forwarder-lt-a3f9")
	}
}

func TestBuildContainer_Defaults(t *testing.T) {
	cfg := SidecarConfig{
		SessionID: "lt-a3f9",
		Target:    "logtap.logtap:9000",
	}
	c := BuildContainer(cfg)

	if c.Image != DefaultImage {
		t.Errorf("Image = %q, want %q", c.Image, DefaultImage)
	}

	memReq := c.Resources.Requests.Memory().String()
	if memReq != DefaultMemReq {
		t.Errorf("memory request = %q, want %q", memReq, DefaultMemReq)
	}

	cpuReq := c.Resources.Requests.Cpu().String()
	if cpuReq != DefaultCPUReq {
		t.Errorf("cpu request = %q, want %q", cpuReq, DefaultCPUReq)
	}
}

func TestBuildContainer_CustomImage(t *testing.T) {
	cfg := SidecarConfig{
		SessionID: "lt-b2c1",
		Target:    "logtap:9000",
		Image:     "custom-image:v1",
	}
	c := BuildContainer(cfg)
	if c.Image != "custom-image:v1" {
		t.Errorf("Image = %q, want %q", c.Image, "custom-image:v1")
	}
}

func TestBuildContainer_EnvVars(t *testing.T) {
	cfg := SidecarConfig{
		SessionID: "lt-a3f9",
		Target:    "logtap.logtap:9000",
	}
	c := BuildContainer(cfg)

	envMap := make(map[string]string)
	envFieldRef := make(map[string]string)
	for _, e := range c.Env {
		if e.ValueFrom != nil && e.ValueFrom.FieldRef != nil {
			envFieldRef[e.Name] = e.ValueFrom.FieldRef.FieldPath
		} else {
			envMap[e.Name] = e.Value
		}
	}

	if envMap["LOGTAP_TARGET"] != "logtap.logtap:9000" {
		t.Errorf("LOGTAP_TARGET = %q, want %q", envMap["LOGTAP_TARGET"], "logtap.logtap:9000")
	}
	if envMap["LOGTAP_SESSION"] != "lt-a3f9" {
		t.Errorf("LOGTAP_SESSION = %q, want %q", envMap["LOGTAP_SESSION"], "lt-a3f9")
	}
	if envFieldRef["LOGTAP_POD_NAME"] != "metadata.name" {
		t.Errorf("LOGTAP_POD_NAME fieldRef = %q, want %q", envFieldRef["LOGTAP_POD_NAME"], "metadata.name")
	}
	if envFieldRef["LOGTAP_NAMESPACE"] != "metadata.namespace" {
		t.Errorf("LOGTAP_NAMESPACE fieldRef = %q, want %q", envFieldRef["LOGTAP_NAMESPACE"], "metadata.namespace")
	}
}

func TestAnnotations(t *testing.T) {
	cfg := SidecarConfig{
		SessionID: "lt-a3f9",
		Target:    "logtap.logtap:9000",
	}
	ann := Annotations(cfg)

	if ann[AnnotationTapped] != "lt-a3f9" {
		t.Errorf("tapped annotation = %q, want %q", ann[AnnotationTapped], "lt-a3f9")
	}
	if ann[AnnotationTarget] != "logtap.logtap:9000" {
		t.Errorf("target annotation = %q, want %q", ann[AnnotationTarget], "logtap.logtap:9000")
	}
}

func TestParseSessions(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"lt-a3f9", []string{"lt-a3f9"}},
		{"lt-a3f9,lt-b2c1", []string{"lt-a3f9", "lt-b2c1"}},
		{"lt-a3f9, lt-b2c1 , lt-c3d2", []string{"lt-a3f9", "lt-b2c1", "lt-c3d2"}},
	}

	for _, tt := range tests {
		got := ParseSessions(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("ParseSessions(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ParseSessions(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestAddSession(t *testing.T) {
	if got := AddSession("", "lt-a3f9"); got != "lt-a3f9" {
		t.Errorf("AddSession empty = %q, want %q", got, "lt-a3f9")
	}
	if got := AddSession("lt-a3f9", "lt-b2c1"); got != "lt-a3f9,lt-b2c1" {
		t.Errorf("AddSession append = %q, want %q", got, "lt-a3f9,lt-b2c1")
	}
}

func TestRemoveSession(t *testing.T) {
	if got := RemoveSession("lt-a3f9,lt-b2c1", "lt-a3f9"); got != "lt-b2c1" {
		t.Errorf("RemoveSession first = %q, want %q", got, "lt-b2c1")
	}
	if got := RemoveSession("lt-a3f9,lt-b2c1", "lt-b2c1"); got != "lt-a3f9" {
		t.Errorf("RemoveSession last = %q, want %q", got, "lt-a3f9")
	}
	if got := RemoveSession("lt-a3f9", "lt-a3f9"); got != "" {
		t.Errorf("RemoveSession only = %q, want empty", got)
	}
	if got := RemoveSession("lt-a3f9,lt-b2c1,lt-c3d2", "lt-b2c1"); got != "lt-a3f9,lt-c3d2" {
		t.Errorf("RemoveSession middle = %q, want %q", got, "lt-a3f9,lt-c3d2")
	}
}

func TestBuildContainer_CustomResources(t *testing.T) {
	cfg := SidecarConfig{
		SessionID:  "lt-a3f9",
		Target:     "logtap:9000",
		MemRequest: "64Mi",
		MemLimit:   "128Mi",
		CPURequest: "100m",
		CPULimit:   "200m",
	}
	c := BuildContainer(cfg)

	if got := c.Resources.Requests.Memory().String(); got != "64Mi" {
		t.Errorf("memory request = %q, want %q", got, "64Mi")
	}
	if got := c.Resources.Limits.Memory().String(); got != "128Mi" {
		t.Errorf("memory limit = %q, want %q", got, "128Mi")
	}
	if got := c.Resources.Requests.Cpu().String(); got != "100m" {
		t.Errorf("cpu request = %q, want %q", got, "100m")
	}
	if got := c.Resources.Limits.Cpu().String(); got != "200m" {
		t.Errorf("cpu limit = %q, want %q", got, "200m")
	}
}
