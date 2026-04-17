// Copyright 2026 Adrien Ndikumana
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prebuiltconfigs

import (
	"embed"
	"fmt"
	"path"
	"strings"
)

//go:embed sidecars
var sidecarsFS embed.FS

// GetSidecar returns the raw YAML bytes of a prebuilt nocfoundry-ops.yaml
// for the given vendor and platform, or nil if none is embedded.
func GetSidecar(vendor, platform string) ([]byte, error) {
	vendor = strings.ToLower(strings.TrimSpace(vendor))
	platform = strings.ToLower(strings.TrimSpace(platform))

	p := path.Join("sidecars", vendor, platform, "nocfoundry-ops.yaml")
	data, err := sidecarsFS.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("no prebuilt sidecar for %s/%s", vendor, platform)
	}
	return data, nil
}
