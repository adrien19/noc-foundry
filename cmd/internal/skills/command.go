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

package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/adrien19/noc-foundry/cmd/internal"
	"github.com/adrien19/noc-foundry/internal/prompts"
	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/spf13/cobra"
)

// skillsCmd is the command for generating skills.
type skillsCmd struct {
	*cobra.Command
	name            string
	description     string
	toolset         string
	outputDir       string
	additionalNotes string
}

// NewCommand creates a new Command.
func NewCommand(opts *internal.NOCFoundryOptions) *cobra.Command {
	cmd := &skillsCmd{}
	cmd.Command = &cobra.Command{
		Use:   "skills-generate",
		Short: "Generate workflow-oriented agent skills from tool configurations",
		RunE: func(c *cobra.Command, args []string) error {
			return run(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&cmd.name, "name", "", "Base name of the generated skill bundle. Used as a prefix when multiple toolsets are emitted.")
	cmd.Flags().StringVar(&cmd.description, "description", "", "Optional description override for single-skill generation.")
	cmd.Flags().StringVar(&cmd.toolset, "toolset", "", "Name of the toolset to convert into a skill. If not provided, one skill is generated per explicit toolset.")
	cmd.Flags().StringVar(&cmd.outputDir, "output-dir", "skills", "Directory to output generated skills")
	cmd.Flags().StringVar(&cmd.additionalNotes, "additional-notes", "", "Additional notes to append to the generated SKILL.md")

	_ = cmd.MarkFlagRequired("name")
	return cmd.Command
}

func run(cmd *skillsCmd, opts *internal.NOCFoundryOptions) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	ctx, shutdown, err := opts.Setup(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = shutdown(ctx)
	}()

	opts.CaptureValidationFlagOverrides(cmd.Command)
	parser := internal.ToolsFileParser{}
	_, err = opts.LoadConfig(ctx, &parser)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cmd.outputDir, 0755); err != nil {
		errMsg := fmt.Errorf("error creating output directory: %w", err)
		opts.Logger.ErrorContext(ctx, errMsg.Error())
		return errMsg
	}

	skillsToGenerate, err := cmd.collectSkills(ctx, opts, parser.EnvVars)
	if err != nil {
		errMsg := fmt.Errorf("error collecting workflows: %w", err)
		opts.Logger.ErrorContext(ctx, errMsg.Error())
		return errMsg
	}
	if len(skillsToGenerate) == 0 {
		opts.Logger.InfoContext(ctx, "No tool workflows found to generate.")
		return nil
	}

	multiSkill := len(skillsToGenerate) > 1
	for _, spec := range skillsToGenerate {
		if err := writeSkillBundle(spec, cmd.outputDir, opts, parser.EnvVars); err != nil {
			opts.Logger.ErrorContext(ctx, err.Error())
			return err
		}
		if multiSkill {
			opts.Logger.InfoContext(ctx, fmt.Sprintf("Generated workflow skill '%s' for toolset '%s'.", spec.Name, spec.ToolsetName))
		} else {
			opts.Logger.InfoContext(ctx, fmt.Sprintf("Generated workflow skill '%s'.", spec.Name))
		}
	}

	return nil
}

func (c *skillsCmd) collectSkills(ctx context.Context, opts *internal.NOCFoundryOptions, envVars map[string]string) ([]skillSpec, error) {
	sourcesMap, authServicesMap, embeddingModelsMap, toolsMap, toolsetsMap, promptsMap, promptsetsMap, err := server.InitializeConfigs(ctx, opts.Cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize resources: %w", err)
	}
	_ = sourcesMap
	_ = authServicesMap
	_ = embeddingModelsMap
	_ = toolsMap
	_ = promptsMap

	toolsetNames, err := c.selectedToolsetNames(opts.Cfg.ToolsetConfigs, toolsetsMap)
	if err != nil {
		return nil, err
	}

	specs := make([]skillSpec, 0, len(toolsetNames))
	multiSkill := len(toolsetNames) > 1
	for _, toolsetName := range toolsetNames {
		spec, err := buildSkillSpec(toolsetName, toolsetsMap, promptsetsMap)
		if err != nil {
			return nil, err
		}
		spec.Name = c.resolvedSkillName(toolsetName, multiSkill)
		spec.Description = c.resolvedDescription(spec, multiSkill)
		spec.AdditionalNotes = c.additionalNotes
		spec.EnvVarNames = sortedEnvNames(envVars)
		specs = append(specs, spec)
	}

	return specs, nil
}

func (c *skillsCmd) selectedToolsetNames(explicit server.ToolsetConfigs, initialized map[string]tools.Toolset) ([]string, error) {
	if c.toolset != "" {
		if _, ok := initialized[c.toolset]; !ok {
			return nil, fmt.Errorf("toolset %q not found", c.toolset)
		}
		return []string{c.toolset}, nil
	}

	explicitNames := make([]string, 0, len(explicit))
	for name := range explicit {
		if name == "" {
			continue
		}
		explicitNames = append(explicitNames, name)
	}
	sort.Strings(explicitNames)
	if len(explicitNames) > 0 {
		return explicitNames, nil
	}

	if _, ok := initialized[""]; !ok {
		return nil, fmt.Errorf("default toolset is unavailable")
	}
	return []string{""}, nil
}

func buildSkillSpec(toolsetName string, toolsetsMap map[string]tools.Toolset, promptsetsMap map[string]prompts.Promptset) (skillSpec, error) {
	ts, ok := toolsetsMap[toolsetName]
	if !ok {
		return skillSpec{}, fmt.Errorf("toolset %q not found", toolsetName)
	}

	spec := skillSpec{
		RawToolsetName: toolsetName,
		ToolsetName:    displayName(toolsetName),
		Description:    ts.Description,
		Tools:          make([]toolBinding, 0, len(ts.Tools)),
	}

	for _, toolPtr := range ts.Tools {
		if toolPtr == nil {
			continue
		}
		tool := *toolPtr
		spec.Tools = append(spec.Tools, toolBinding{
			Name: tool.McpManifest().Name,
			Tool: tool,
		})
	}

	if ts.Promptset != "" {
		ps, ok := promptsetsMap[ts.Promptset]
		if !ok {
			return skillSpec{}, fmt.Errorf("toolset %q references promptset %q, which does not exist", displayName(toolsetName), ts.Promptset)
		}
		spec.PromptsetName = ts.Promptset
		spec.Prompts = make([]promptBinding, 0, len(ps.Prompts))
		for _, promptPtr := range ps.Prompts {
			if promptPtr == nil {
				continue
			}
			prompt := *promptPtr
			spec.Prompts = append(spec.Prompts, promptBinding{
				Name:   prompt.McpManifest().Name,
				Prompt: prompt,
			})
		}
	}

	return spec, nil
}

func (c *skillsCmd) resolvedSkillName(toolsetName string, multiSkill bool) string {
	if !multiSkill {
		return c.name
	}
	return fmt.Sprintf("%s-%s", c.name, displayName(toolsetName))
}

func (c *skillsCmd) resolvedDescription(spec skillSpec, multiSkill bool) string {
	if !multiSkill && strings.TrimSpace(c.description) != "" {
		return c.description
	}
	if strings.TrimSpace(spec.Description) != "" {
		return spec.Description
	}
	if spec.RawToolsetName == "" {
		return "Network operations agent skill generated from all available tools."
	}
	return fmt.Sprintf("Network operations agent skill for the %s workflow.", spec.ToolsetName)
}

func writeSkillBundle(spec skillSpec, outputDir string, opts *internal.NOCFoundryOptions, envVars map[string]string) error {
	skillPath := filepath.Join(outputDir, spec.Name)
	if err := os.MkdirAll(skillPath, 0755); err != nil {
		return fmt.Errorf("error creating skill directory %q: %w", skillPath, err)
	}

	assetsPath := filepath.Join(skillPath, "assets")
	if err := os.MkdirAll(assetsPath, 0755); err != nil {
		return fmt.Errorf("error creating assets directory %q: %w", assetsPath, err)
	}

	executionArgs, assets, err := materializeAssets(opts, assetsPath)
	if err != nil {
		return err
	}
	spec.ExecutionArgs = executionArgs
	spec.Assets = assets

	skillContent, err := generateSkillMarkdown(spec, envVars)
	if err != nil {
		return fmt.Errorf("error generating SKILL.md content for %q: %w", spec.Name, err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		return fmt.Errorf("error writing SKILL.md for %q: %w", spec.Name, err)
	}

	manifestContent, err := generateSkillManifest(spec)
	if err != nil {
		return fmt.Errorf("error generating skill.yaml content for %q: %w", spec.Name, err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "skill.yaml"), manifestContent, 0644); err != nil {
		return fmt.Errorf("error writing skill.yaml for %q: %w", spec.Name, err)
	}

	return nil
}

func materializeAssets(opts *internal.NOCFoundryOptions, assetsPath string) ([]string, []assetReference, error) {
	args := make([]string, 0)
	assets := make([]assetReference, 0)

	for _, pc := range opts.PrebuiltConfigs {
		args = append(args, "--prebuilt", pc)
		assets = append(assets, assetReference{
			Type: "prebuilt",
			Name: pc,
		})
	}

	if opts.ToolsFolder != "" {
		folderName := filepath.Base(opts.ToolsFolder)
		destFolder := filepath.Join(assetsPath, folderName)
		if err := copyDir(opts.ToolsFolder, destFolder); err != nil {
			return nil, nil, err
		}
		relPath := filepath.ToSlash(filepath.Join("assets", folderName))
		args = append(args, "--tools-folder", relPath)
		assets = append(assets, assetReference{
			Type: "folder",
			Path: relPath,
		})
	} else if len(opts.ToolsFiles) > 0 {
		for _, f := range opts.ToolsFiles {
			baseName := filepath.Base(f)
			destPath := filepath.Join(assetsPath, baseName)
			if err := copyFile(f, destPath); err != nil {
				return nil, nil, err
			}
			relPath := filepath.ToSlash(filepath.Join("assets", baseName))
			args = append(args, "--tools-files", relPath)
			assets = append(assets, assetReference{
				Type: "file",
				Path: relPath,
			})
		}
	} else if opts.ToolsFile != "" {
		baseName := filepath.Base(opts.ToolsFile)
		destPath := filepath.Join(assetsPath, baseName)
		if err := copyFile(opts.ToolsFile, destPath); err != nil {
			return nil, nil, err
		}
		relPath := filepath.ToSlash(filepath.Join("assets", baseName))
		args = append(args, "--tools-file", relPath)
		assets = append(assets, assetReference{
			Type: "file",
			Path: relPath,
		})
	}

	return slices.Clone(args), assets, nil
}

func displayName(name string) string {
	if name == "" {
		return "default"
	}
	return name
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, info.Mode())
	})
}
