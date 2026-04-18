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
			filter, renderWarnings, err := renderNetconfKeys(pp.Filter, pp.Parameters, values, provided)
			if err != nil {
				if hasSafetyLimits(pp.Limits) && !opts.AllowUnsafeFullFetch {
					return pp, nil, err
				}
				warnings = append(warnings, err.Error())
			} else {
				rendered.Filter = filter
			}
			warnings = append(warnings, renderWarnings...)
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

func renderNetconfKeys(filter string, params []profiles.OperationParameter, values map[string]string, provided map[string]bool) (string, []string, error) {
	if filter == "" || len(provided) == 0 {
		return filter, nil, nil
	}
	byContainer := map[string][]string{}
	for _, p := range params {
		if !provided[p.Name] || p.PathKey == "" {
			continue
		}
		container := p.TargetContainer
		if container == "" {
			container = containerFromTargetPath(p.TargetPath)
		}
		if container == "" {
			return filter, nil, fmt.Errorf("NETCONF parameter %q cannot be rendered without target_container or target_path", p.Name)
		}
		byContainer[container] = append(byContainer[container], fmt.Sprintf("<%s>%s</%s>", p.PathKey, values[p.Name], p.PathKey))
	}
	rendered := filter
	for container, leaves := range byContainer {
		var err error
		rendered, err = insertNetconfLeaves(rendered, container, strings.Join(leaves, ""))
		if err != nil {
			return filter, nil, err
		}
	}
	return rendered, []string{"source-side NETCONF filtering used generic subtree key insertion; add netconf_filter_template for exact vendor rendering if this filter does not resolve"}, nil
}

func containerFromTargetPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if idx := strings.Index(last, "["); idx >= 0 {
		last = last[:idx]
	}
	if idx := strings.Index(last, ":"); idx >= 0 {
		last = last[idx+1:]
	}
	return last
}

func insertNetconfLeaves(filter, container, leaves string) (string, error) {
	filter = expandSelfClosingNetconfTarget(filter, container)
	startRe := regexp.MustCompile(`<([A-Za-z_][A-Za-z0-9_.:-]*)(?:\s[^>]*)?>`)
	matches := startRe.FindAllStringSubmatchIndex(filter, -1)
	var target []int
	for _, match := range matches {
		name := filter[match[2]:match[3]]
		if localXMLName(name) == container {
			target = match
		}
	}
	if target == nil {
		return filter, fmt.Errorf("NETCONF filter target container %q was not found", container)
	}
	count := 0
	for _, match := range matches {
		name := filter[match[2]:match[3]]
		if localXMLName(name) == container {
			count++
		}
	}
	if count != 1 {
		return filter, fmt.Errorf("NETCONF filter target container %q matched %d elements; add netconf_filter_template", container, count)
	}
	insertAt := target[1]
	return filter[:insertAt] + leaves + filter[insertAt:], nil
}

func expandSelfClosingNetconfTarget(filter, container string) string {
	selfCloseRe := regexp.MustCompile(`<([A-Za-z_][A-Za-z0-9_.:-]*)([^>]*)/>`)
	return selfCloseRe.ReplaceAllStringFunc(filter, func(match string) string {
		parts := selfCloseRe.FindStringSubmatch(match)
		if len(parts) < 3 || localXMLName(parts[1]) != container {
			return match
		}
		return "<" + parts[1] + parts[2] + "></" + parts[1] + ">"
	})
}

func localXMLName(name string) string {
	if idx := strings.Index(name, ":"); idx >= 0 {
		return name[idx+1:]
	}
	return name
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
