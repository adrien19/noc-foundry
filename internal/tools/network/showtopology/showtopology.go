package showtopology

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/tools/network/profilequery"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/goccy/go-yaml"
)

const kind = "network-show-topology"

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
}

type Config struct {
	Name           string                       `yaml:"name" validate:"required"`
	Type           string                       `yaml:"type" validate:"required"`
	SourceSelector *profilequery.SourceSelector `yaml:"sourceSelector" validate:"required"`
	Description    string                       `yaml:"description"`
	AuthRequired   []string                     `yaml:"authRequired"`
	Annotations    *tools.ToolAnnotations       `yaml:"annotations"`
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name, Type: kind}
	if decoder != nil {
		if err := decoder.DecodeContext(ctx, &actual); err != nil {
			return nil, err
		}
	}
	return actual, nil
}

func (cfg Config) ToolConfigType() string { return kind }

func (cfg Config) Initialize(_ map[string]sources.Source) (tools.Tool, error) {
	if cfg.SourceSelector == nil {
		return nil, fmt.Errorf("tool %q must specify sourceSelector", cfg.Name)
	}
	desc := cfg.Description
	if desc == "" {
		desc = "Build an LLDP-derived topology map for devices matching a label selector."
	}
	params := parameters.Parameters{}
	annotations := tools.GetAnnotationsOrDefault(cfg.Annotations, tools.NewReadOnlyAnnotations)
	return Tool{
		Config:      cfg,
		executor:    query.NewExecutor(),
		manifest:    tools.Manifest{Description: desc, Parameters: params.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, params, annotations),
		Parameters:  params,
	}, nil
}

type Tool struct {
	Config
	executor    *query.Executor
	manifest    tools.Manifest
	mcpManifest tools.McpManifest
	Parameters  parameters.Parameters
}

type Result struct {
	Topology models.TopologyMap    `json:"topology"`
	Errors   []fanout.DeviceResult `json:"errors,omitempty"`
}

func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	srcs, err := resourceMgr.GetSourcesByLabels(ctx, selectorLabels(t.SourceSelector))
	if err != nil {
		return nil, util.NewClientServerError("failed to resolve topology sources", http.StatusInternalServerError, err)
	}
	if len(srcs) == 0 {
		return nil, util.NewClientServerError("no sources matched topology selector", http.StatusNotFound, fmt.Errorf("sourceSelector matched 0 devices"))
	}
	sourceNames := make([]string, 0, len(srcs))
	for name := range srcs {
		sourceNames = append(sourceNames, name)
	}
	maxConc := fanout.DefaultMaxConcurrency
	if t.SourceSelector.MaxConcurrency > 0 {
		maxConc = t.SourceSelector.MaxConcurrency
	}
	result := fanout.Execute(ctx, sourceNames, maxConc, func(ctx context.Context, sourceName string) (any, error) {
		rawSource, ok := resourceMgr.GetSource(sourceName)
		if !ok {
			return nil, fmt.Errorf("source %q not found", sourceName)
		}
		return t.executor.ExecuteWithOptions(ctx, rawSource, profiles.OpGetLLDPNeighbors, sourceName, query.ExecuteOptions{})
	})
	return buildTopologyResult(result, labelsForSources(srcs, resourceMgr.GetDevicePoolLabels())), nil
}

func buildTopologyResult(result fanout.Result, labels map[string]map[string]string) Result {
	nodes := map[string]models.TopologyNode{}
	for sourceName, nodeLabels := range labels {
		device := extractDevice(sourceName)
		nodes[device] = models.TopologyNode{Device: device, Labels: nodeLabels}
	}
	var links []models.TopologyLink
	var errors []fanout.DeviceResult
	for _, deviceResult := range result.Results {
		if deviceResult.Status != "success" {
			errors = append(errors, deviceResult)
			continue
		}
		record, ok := deviceResult.Data.(*models.Record)
		if !ok {
			errors = append(errors, fanout.DeviceResult{Device: deviceResult.Device, Status: "error", Error: "LLDP result was not a canonical record"})
			continue
		}
		for _, neighbor := range lldpNeighbors(record.Payload) {
			link := newTopologyLink(deviceResult.Device, neighbor, nodes)
			links = append(links, link)
		}
	}
	markBidirectional(links)
	topologyNodes := make([]models.TopologyNode, 0, len(nodes))
	for _, node := range nodes {
		topologyNodes = append(topologyNodes, node)
	}
	sort.Slice(topologyNodes, func(i, j int) bool {
		return topologyNodes[i].Device < topologyNodes[j].Device
	})
	sort.Slice(links, func(i, j int) bool {
		a, b := links[i], links[j]
		if a.LocalDevice != b.LocalDevice {
			return a.LocalDevice < b.LocalDevice
		}
		if a.LocalInterface != b.LocalInterface {
			return a.LocalInterface < b.LocalInterface
		}
		if a.RemoteDevice != b.RemoteDevice {
			return a.RemoteDevice < b.RemoteDevice
		}
		return a.RemoteInterface < b.RemoteInterface
	})
	return Result{Topology: models.TopologyMap{Nodes: topologyNodes, Links: links}, Errors: errors}
}

func newTopologyLink(localDevice string, neighbor models.LLDPNeighbor, nodes map[string]models.TopologyNode) models.TopologyLink {
	link := models.TopologyLink{
		LocalDevice:     localDevice,
		LocalInterface:  neighbor.LocalInterface,
		RemoteDevice:    neighbor.RemoteSystemName,
		RemoteInterface: neighbor.RemotePortID,
		Confidence:      "low",
		Evidence:        []string{"lldp_single_sided"},
	}
	if link.RemoteInterface == "" {
		link.Evidence = appendEvidence(link.Evidence, "remote_interface_missing")
	}
	if _, ok := nodes[link.RemoteDevice]; ok && link.RemoteDevice != "" {
		link.Confidence = "medium"
		link.Evidence = appendEvidence(link.Evidence, "inventory_node_matched")
	} else {
		link.Evidence = appendEvidence(link.Evidence, "unmatched_inventory_node")
	}
	return link
}

func lldpNeighbors(payload any) []models.LLDPNeighbor {
	if neighbors, ok := payload.([]models.LLDPNeighbor); ok {
		return neighbors
	}
	return nil
}

func markBidirectional(links []models.TopologyLink) {
	for i := range links {
		for j := range links {
			if i == j {
				continue
			}
			if !isBidirectionalPair(links[i], links[j]) {
				continue
			}
			links[i].Confidence = "high"
			links[i].Evidence = appendEvidence(links[i].Evidence, "lldp_bidirectional")
			break
		}
	}
}

func isBidirectionalPair(a, b models.TopologyLink) bool {
	if a.LocalDevice == "" || a.RemoteDevice == "" || b.LocalDevice == "" || b.RemoteDevice == "" {
		return false
	}
	if a.LocalDevice != b.RemoteDevice || a.RemoteDevice != b.LocalDevice {
		return false
	}
	// TODO(topology-confidence): Reconcile LLDP remote-system-name matching
	// with remote chassis/source IDs when hostname-based identity is not stable
	// enough to prove that two records describe the same managed node.
	if a.RemoteInterface != "" && b.LocalInterface != "" && a.RemoteInterface != b.LocalInterface {
		return false
	}
	if b.RemoteInterface != "" && a.LocalInterface != "" && b.RemoteInterface != a.LocalInterface {
		return false
	}
	return true
}

func appendEvidence(existing []string, value string) []string {
	for _, item := range existing {
		if item == value {
			return existing
		}
	}
	return append(existing, value)
}

func selectorLabels(selector *profilequery.SourceSelector) map[string]string {
	matchLabels := selector.MatchLabels
	if selector.Template == "" {
		return matchLabels
	}
	merged := make(map[string]string, len(matchLabels)+1)
	for k, v := range matchLabels {
		merged[k] = v
	}
	merged["template"] = selector.Template
	return merged
}

func labelsForSources(srcs map[string]sources.Source, all map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(srcs))
	for name := range srcs {
		out[name] = all[name]
	}
	return out
}

func extractDevice(sourceName string) string {
	parts := strings.Split(sourceName, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return sourceName
}

func (t Tool) Manifest() tools.Manifest          { return t.manifest }
func (t Tool) McpManifest() tools.McpManifest    { return t.mcpManifest }
func (t Tool) Authorized(services []string) bool { return tools.IsAuthorized(t.AuthRequired, services) }
func (t Tool) RequiresClientAuthorization(resourceMgr tools.SourceProvider) (bool, error) {
	return false, nil
}
func (t Tool) ToConfig() tools.ToolConfig { return t.Config }
func (t Tool) GetAuthTokenHeaderName(resourceMgr tools.SourceProvider) (string, error) {
	return "Authorization", nil
}
func (t Tool) GetParameters() parameters.Parameters { return t.Parameters }
func (t Tool) EmbedParams(ctx context.Context, paramValues parameters.ParamValues, embeddingModelsMap map[string]embeddingmodels.EmbeddingModel) (parameters.ParamValues, error) {
	return paramValues, nil
}
