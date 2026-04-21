package schemas

import (
	"os"
	"testing"

	"github.com/adrien19/noc-foundry/internal/prebuiltconfigs"
	"github.com/goccy/go-yaml"
)

func TestNokiaSRLinuxPrebuiltSidecar_RealYangVersions(t *testing.T) {
	base := os.Getenv("NOCFOUNDRY_NOKIA_SRLINUX_YANG_DIR")
	if base == "" {
		t.Skip("set NOCFOUNDRY_NOKIA_SRLINUX_YANG_DIR to a directory containing v24.10 and v25.10 SR Linux YANG trees")
	}
	for _, version := range []string{"v24.10", "v25.10"} {
		t.Run(version, func(t *testing.T) {
			store := NewSchemaStore()
			key := SchemaKey{Vendor: "nokia", Platform: "srlinux", Version: version}
			if err := store.Load(key, []string{base + "/" + version}); err != nil {
				t.Fatalf("Load(%s) failed: %v", version, err)
			}
			sidecarData, err := prebuiltconfigs.GetSidecar("nokia", "srlinux")
			if err != nil {
				t.Fatal(err)
			}
			var ops SidecarOps
			if err := yaml.Unmarshal(sidecarData, &ops); err != nil {
				t.Fatal(err)
			}
			mappings := ops.ToOperationMappings()
			bundle, _ := store.Lookup("nokia", "srlinux", version)
			validations := bundle.ValidateOperationPaths(mappings)
			var unresolved []string
			for _, result := range validations {
				if result.Status == PathNotFound {
					unresolved = append(unresolved, result.Path)
				}
			}
			if len(unresolved) > 0 {
				t.Fatalf("unresolved SR Linux %s paths: %v", version, unresolved)
			}
		})
	}
}
