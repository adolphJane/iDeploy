package server

import (
	"encoding/json"
	"strings"
	"testing"
	"text/template"
)

func TestRuntimeProjectionHandlesBindMountsWithoutName(t *testing.T) {
	render, err := template.New("docker").Option("missingkey=error").Funcs(template.FuncMap{
		"json": func(value any) (string, error) {
			encoded, err := json.Marshal(value)
			return string(encoded), err
		},
	}).Parse(runtimeDetailFormat)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{
		"Id": strings.Repeat("c", 64), "Image": "sha256:" + strings.Repeat("a", 64),
		"Config": map[string]any{"Image": "registry.test/app:1.0.0", "Labels": nil, "Env": []string{"fixture-unused"}},
		"Mounts": []map[string]any{{"Type": "bind", "Source": "/fixture/data", "Destination": "/data", "RW": false}},
	}
	var output strings.Builder
	if err := render.Execute(&output, input); err != nil {
		t.Fatal(err)
	}
	var observed ContainerRuntimeDetails
	if err := json.Unmarshal([]byte(output.String()), &observed); err != nil {
		t.Fatal(err)
	}
	if len(observed.Mounts) != 1 || observed.Mounts[0].Name != "" || observed.Mounts[0].Source != "/fixture/data" || observed.Mounts[0].RW || strings.Contains(output.String(), "fixture-unused") {
		t.Fatalf("unexpected projected state: %s", output.String())
	}
}

func TestContainerMountArgumentRejectsDockerOptionInjection(t *testing.T) {
	for _, source := range []string{"/data,readonly", "/data\n", "/data/../other"} {
		value := ContainerMountSpec{Type: "bind", Source: source, Destination: "/app/data"}
		if _, err := value.Argument(); err == nil {
			t.Fatalf("accepted unsafe mount source %q", source)
		}
	}
	spec := ContainerMountSpec{Type: "volume", Source: "app_data", Destination: "/app/data", ReadOnly: true}
	got, err := spec.Argument()
	if err != nil || got != "type=volume,source=app_data,target=/app/data,volume-nocopy,readonly" {
		t.Fatalf("unexpected named volume argument %q: %v", got, err)
	}
}
