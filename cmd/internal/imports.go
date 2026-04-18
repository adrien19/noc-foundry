// Copyright 2026 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
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

package internal

import (
	// Import profile packages for side effect of registration
	_ "github.com/adrien19/noc-foundry/internal/network/profiles"

	// Import inventory provider packages for side effect of registration
	_ "github.com/adrien19/noc-foundry/internal/devicegroups/providers/netbox"

	// Import prompt packages for side effect of registration
	_ "github.com/adrien19/noc-foundry/internal/prompts/custom"

	// Import tool packages for side effect of registration
	_ "github.com/adrien19/noc-foundry/internal/tools/common/validate"
	_ "github.com/adrien19/noc-foundry/internal/tools/common/validationruns"
	_ "github.com/adrien19/noc-foundry/internal/tools/http"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/diagnostictools"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/listdevices"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/profiletools"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/query"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/show"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/showalarms"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/showbgpneighbors"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/showcoverage"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/showinterfaces"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/showroutes"
	_ "github.com/adrien19/noc-foundry/internal/tools/network/showversion"
	// TODO(ops-readiness): Add network-show-topology and network-compare after
	// LLDP/tool coverage has confidence/evidence semantics and fleet fanout can
	// return structured per-device partial results instead of failing whole
	// aggregations.
	_ "github.com/adrien19/noc-foundry/internal/tools/utility/wait"

	// Import source packages for side effect of registration
	_ "github.com/adrien19/noc-foundry/internal/sources/gnmi"
	_ "github.com/adrien19/noc-foundry/internal/sources/http"
	_ "github.com/adrien19/noc-foundry/internal/sources/netconf"
	_ "github.com/adrien19/noc-foundry/internal/sources/ssh"

	// Import auth service packages for side effect of registration
	_ "github.com/adrien19/noc-foundry/internal/auth/oidc"
)
