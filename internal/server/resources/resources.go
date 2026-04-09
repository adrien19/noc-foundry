// Copyright 2025 Google LLC
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

package resources

import (
	"context"
	"sync"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/adrien19/noc-foundry/internal/devicegroups"
	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/prompts"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/validationruns"
)

// ResourceManager contains available resources for the server. Should be initialized with NewResourceManager().
type ResourceManager struct {
	mu              sync.RWMutex
	sources         map[string]sources.Source
	authServices    map[string]auth.AuthService
	embeddingModels map[string]embeddingmodels.EmbeddingModel
	tools           map[string]tools.Tool
	toolsets        map[string]tools.Toolset
	prompts         map[string]prompts.Prompt
	promptsets      map[string]prompts.Promptset
	devicePool      *devicegroups.DevicePool
	validationRuns  validationruns.Manager
	resourceVersion uint64
}

func NewResourceManager(
	sourcesMap map[string]sources.Source,
	authServicesMap map[string]auth.AuthService,
	embeddingModelsMap map[string]embeddingmodels.EmbeddingModel,
	toolsMap map[string]tools.Tool, toolsetsMap map[string]tools.Toolset,
	promptsMap map[string]prompts.Prompt, promptsetsMap map[string]prompts.Promptset,

) *ResourceManager {
	resourceMgr := &ResourceManager{
		mu:              sync.RWMutex{},
		sources:         sourcesMap,
		authServices:    authServicesMap,
		embeddingModels: embeddingModelsMap,
		tools:           toolsMap,
		toolsets:        toolsetsMap,
		prompts:         promptsMap,
		promptsets:      promptsetsMap,
		resourceVersion: 1,
	}

	return resourceMgr
}

func (r *ResourceManager) GetSource(sourceName string) (sources.Source, bool) {
	r.mu.RLock()
	source, ok := r.sources[sourceName]
	r.mu.RUnlock()
	if ok {
		return source, true
	}
	// Fallback to device pool for lazy source creation
	if r.devicePool != nil && r.devicePool.HasSource(sourceName) {
		s, err := r.devicePool.GetOrCreate(context.Background(), sourceName)
		if err != nil {
			return nil, false
		}
		return s, true
	}
	return nil, false
}

// SetDevicePool sets the device pool for lazy source creation.
func (r *ResourceManager) SetDevicePool(pool *devicegroups.DevicePool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devicePool = pool
}

func (r *ResourceManager) SetValidationRunManager(manager validationruns.Manager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validationRuns = manager
}

func (r *ResourceManager) GetValidationRunManager() validationruns.Manager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.validationRuns
}

func (r *ResourceManager) GetResourceVersion() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resourceVersion
}

// GetSourcesBySelector returns all sources matching the given label selector.
// Sources are created lazily from device groups if needed.
func (r *ResourceManager) GetSourcesBySelector(ctx context.Context, selector devicegroups.LabelSelector) (map[string]sources.Source, error) {
	if r.devicePool == nil {
		return nil, nil
	}
	names := r.devicePool.SelectSources(selector)
	result := make(map[string]sources.Source, len(names))
	for _, name := range names {
		s, err := r.devicePool.GetOrCreate(ctx, name)
		if err != nil {
			return nil, err
		}
		result[name] = s
	}
	return result, nil
}

// GetSourcesByLabels implements tools.SourceProvider. It translates the raw
// label map into a LabelSelector and delegates to GetSourcesBySelector.
func (r *ResourceManager) GetSourcesByLabels(ctx context.Context, matchLabels map[string]string) (map[string]sources.Source, error) {
	return r.GetSourcesBySelector(ctx, devicegroups.LabelSelector{MatchLabels: matchLabels})
}

// GetDevicePoolLabels returns the label index from the device pool, or nil.
func (r *ResourceManager) GetDevicePoolLabels() map[string]map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.devicePool == nil {
		return nil
	}
	return r.devicePool.Labels()
}

func (r *ResourceManager) GetAuthService(authServiceName string) (auth.AuthService, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	authService, ok := r.authServices[authServiceName]
	return authService, ok
}

func (r *ResourceManager) GetEmbeddingModel(embeddingModelName string) (embeddingmodels.EmbeddingModel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	model, ok := r.embeddingModels[embeddingModelName]
	return model, ok
}

func (r *ResourceManager) GetTool(toolName string) (tools.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[toolName]
	return tool, ok
}

func (r *ResourceManager) GetToolset(toolsetName string) (tools.Toolset, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	toolset, ok := r.toolsets[toolsetName]
	return toolset, ok
}

func (r *ResourceManager) GetPrompt(promptName string) (prompts.Prompt, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	prompt, ok := r.prompts[promptName]
	return prompt, ok
}

func (r *ResourceManager) GetPromptset(promptsetName string) (prompts.Promptset, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	promptset, ok := r.promptsets[promptsetName]
	return promptset, ok
}

func (r *ResourceManager) SetResources(sourcesMap map[string]sources.Source, authServicesMap map[string]auth.AuthService, embeddingModelsMap map[string]embeddingmodels.EmbeddingModel, toolsMap map[string]tools.Tool, toolsetsMap map[string]tools.Toolset, promptsMap map[string]prompts.Prompt, promptsetsMap map[string]prompts.Promptset) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources = sourcesMap
	r.authServices = authServicesMap
	r.embeddingModels = embeddingModelsMap
	r.tools = toolsMap
	r.toolsets = toolsetsMap
	r.prompts = promptsMap
	r.promptsets = promptsetsMap
	r.resourceVersion++
}

func (r *ResourceManager) GetAuthServiceMap() map[string]auth.AuthService {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copiedMap := make(map[string]auth.AuthService, len(r.authServices))
	for k, v := range r.authServices {
		copiedMap[k] = v
	}
	return copiedMap
}

func (r *ResourceManager) GetEmbeddingModelMap() map[string]embeddingmodels.EmbeddingModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copiedMap := make(map[string]embeddingmodels.EmbeddingModel, len(r.embeddingModels))
	for k, v := range r.embeddingModels {
		copiedMap[k] = v
	}
	return copiedMap
}

func (r *ResourceManager) GetToolsMap() map[string]tools.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copiedMap := make(map[string]tools.Tool, len(r.tools))
	for k, v := range r.tools {
		copiedMap[k] = v
	}
	return copiedMap
}

func (r *ResourceManager) GetToolsetsMap() map[string]tools.Toolset {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copiedMap := make(map[string]tools.Toolset, len(r.toolsets))
	for k, v := range r.toolsets {
		copiedMap[k] = v
	}
	return copiedMap
}

func (r *ResourceManager) GetPromptsMap() map[string]prompts.Prompt {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copiedMap := make(map[string]prompts.Prompt, len(r.prompts))
	for k, v := range r.prompts {
		copiedMap[k] = v
	}
	return copiedMap
}

func (r *ResourceManager) GetPromptsetsMap() map[string]prompts.Promptset {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copiedMap := make(map[string]prompts.Promptset, len(r.promptsets))
	for k, v := range r.promptsets {
		copiedMap[k] = v
	}
	return copiedMap
}
