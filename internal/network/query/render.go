package query

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

var (
	yangParamSafeRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_\-.:@]*$`)
	templateVarRe   = regexp.MustCompile(`\{([a-zA-Z0-9_\-]+)\}`)
)

func renderProtocolPath(pp profiles.ProtocolPath, opts ExecuteOptions) (profiles.ProtocolPath, []string, error) {
	if len(pp.Parameters) == 0 {
		return pp, nil, nil
	}

	values, provided, err := validateOperationParams(pp.Parameters, opts.Params)
	if err != nil {
		return pp, nil, err
	}
	rendered := pp
	var warnings []string

	switch pp.Protocol {
	case profiles.ProtocolGnmiOpenConfig, profiles.ProtocolGnmiNative:
		if hasGnmiTemplates(pp.Parameters) {
			rendered.Paths = renderGnmiTemplates(pp.Paths, pp.Parameters, values, provided)
		} else if len(provided) > 0 {
			rendered.Paths, warnings = appendGnmiKeys(pp.Paths, pp.Parameters, values, provided)
		}
	case profiles.ProtocolNetconfOpenConfig, profiles.ProtocolNetconfNative:
		if template := netconfTemplate(pp.Parameters); template != "" {
			rendered.Filter = renderTemplate(template, values)
		} else if len(provided) > 0 {
			rendered.Filter, warnings = addNetconfKeyHints(pp.Filter, pp.Parameters, values, provided)
		}
	case profiles.ProtocolCLI:
		if strings.Contains(rendered.Command, "{") {
			rendered.Command = renderTemplate(rendered.Command, values)
		}
	}
	if len(provided) == 0 && hasSafetyLimits(pp.Limits) && !opts.AllowUnsafeFullFetch {
		return pp, nil, fmt.Errorf("operation has safety limits and no source-side filter parameters were provided; pass a supported filter or explicitly allow unsafe full fetch")
	}
	return rendered, warnings, nil
}

func validateOperationParams(params []profiles.OperationParameter, raw map[string]any) (map[string]string, map[string]bool, error) {
	values := make(map[string]string, len(params))
	provided := make(map[string]bool, len(params))
	for _, p := range params {
		value, ok := raw[p.Name]
		if !ok || value == nil || fmt.Sprint(value) == "" {
			if p.Default != "" {
				values[p.Name] = p.Default
				continue
			}
			if p.Required {
				return nil, nil, fmt.Errorf("missing required operation parameter %q", p.Name)
			}
			continue
		}
		s := fmt.Sprint(value)
		if !yangParamSafeRe.MatchString(s) {
			return nil, nil, fmt.Errorf("operation parameter %q contains unsafe characters", p.Name)
		}
		if len(p.Allowed) > 0 && !stringIn(s, p.Allowed) {
			return nil, nil, fmt.Errorf("operation parameter %q value %q is not allowed", p.Name, s)
		}
		values[p.Name] = s
		provided[p.Name] = true
	}
	return values, provided, nil
}

func hasGnmiTemplates(params []profiles.OperationParameter) bool {
	for _, p := range params {
		if p.GnmiPathTemplate != "" {
			return true
		}
	}
	return false
}

func renderGnmiTemplates(base []string, params []profiles.OperationParameter, values map[string]string, provided map[string]bool) []string {
	var out []string
	for _, p := range params {
		if p.GnmiPathTemplate != "" && shouldRenderTemplate(p, values, provided) {
			out = append(out, renderTemplate(p.GnmiPathTemplate, values))
		}
	}
	if len(out) == 0 {
		return base
	}
	return out
}

func shouldRenderTemplate(param profiles.OperationParameter, values map[string]string, provided map[string]bool) bool {
	if !provided[param.Name] && !param.Required && param.Default == "" {
		return false
	}
	for _, match := range templateVarRe.FindAllStringSubmatch(param.GnmiPathTemplate, -1) {
		if len(match) < 2 {
			continue
		}
		if values[match[1]] == "" {
			return false
		}
	}
	return true
}

func appendGnmiKeys(paths []string, params []profiles.OperationParameter, values map[string]string, provided map[string]bool) ([]string, []string) {
	rendered := make([]string, 0, len(paths))
	var warnings []string
	for _, path := range paths {
		next := path
		for _, p := range params {
			if !provided[p.Name] || p.PathKey == "" {
				continue
			}
			if p.TargetPath != "" && !strings.Contains(path, p.TargetPath) {
				continue
			}
			needle := "[" + p.PathKey + "="
			if strings.Contains(next, needle) {
				continue
			}
			next += "[" + p.PathKey + "=" + values[p.Name] + "]"
		}
		rendered = append(rendered, next)
	}
	if len(provided) > 0 {
		warnings = append(warnings, "source-side gNMI filtering used generic key appending; add gnmi_path_template for exact vendor path rendering if this path does not resolve")
	}
	return rendered, warnings
}

func netconfTemplate(params []profiles.OperationParameter) string {
	for _, p := range params {
		if p.NetconfFilterTemplate != "" {
			return p.NetconfFilterTemplate
		}
	}
	return ""
}

func addNetconfKeyHints(filter string, params []profiles.OperationParameter, values map[string]string, provided map[string]bool) (string, []string) {
	if filter == "" || len(provided) == 0 {
		return filter, nil
	}
	// TODO(schema-ops): Replace comment hints with real subtree predicate
	// rendering for NETCONF when no netconf_filter_template is declared.
	// Post-filtering large route/RIB/log/config tables is unsafe; high-volume
	// NETCONF operations need sidecar templates or schema-aware subtree key
	// insertion before they are fully Ops-ready on NETCONF-only sources.
	var hints strings.Builder
	for _, p := range params {
		if provided[p.Name] && p.PathKey != "" {
			hints.WriteString(fmt.Sprintf("<!-- nocfoundry filter %s=%s -->", p.PathKey, values[p.Name]))
		}
	}
	return hints.String() + filter, []string{"NETCONF parameter binding could not safely rewrite subtree filter; add netconf_filter_template for exact source-side filtering"}
}

func renderTemplate(template string, values map[string]string) string {
	out := template
	for k, v := range values {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

func hasSafetyLimits(limits *profiles.OperationLimits) bool {
	return limits != nil && (limits.DefaultCount > 0 || limits.MaxCount > 0 || limits.MaxBytes > 0)
}

func stringIn(value string, allowed []string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}
