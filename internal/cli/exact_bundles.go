package cli

import (
	"fmt"
	"strings"

	"paracetamol/internal/catalog"
	"paracetamol/internal/controlerr"
	"paracetamol/internal/ui"
)

type exactBundleCategory struct {
	ID          string
	Description string
}

var exactBundleCategories = []exactBundleCategory{
	{ID: "comfyui-images", Description: "ComfyUI — image models"},
	{ID: "comfyui-videos", Description: "ComfyUI — video models"},
	{ID: "comfyui-addons", Description: "ComfyUI — workflows and add-ons"},
	{ID: "llama-cpp", Description: "llama.cpp"},
	{ID: "dwarfstar", Description: "DwarfStar"},
}

func categoryForExactBundle(bundle catalog.Bundle) (string, error) {
	if bundle.Application == "llama-cpp" || bundle.Application == "dwarfstar" {
		return bundle.Application, nil
	}
	if bundle.Application != "comfyui" {
		return "", fmt.Errorf("exact bundle %q has no browser category", bundle.ID)
	}
	groups := make(map[string]bool, len(bundle.Groups))
	for _, group := range bundle.Groups {
		groups[group] = true
	}
	var matches []string
	if groups["qwen"] || groups["krea"] {
		matches = append(matches, "comfyui-images")
	}
	if groups["wan"] || groups["ltx"] || groups["hunyuan"] {
		matches = append(matches, "comfyui-videos")
	}
	if groups["ltx-camera"] {
		matches = append(matches, "comfyui-addons")
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("exact bundle %q must have exactly one browser category", bundle.ID)
	}
	return matches[0], nil
}

func exactBundlesInCategory(managed catalog.Catalog, category string) ([]catalog.Bundle, error) {
	identifiers := sortedBundleIDs(managed)
	var result []catalog.Bundle
	for _, identifier := range identifiers {
		bundle := managed.Bundles[identifier]
		actual, err := categoryForExactBundle(bundle)
		if err != nil {
			return nil, err
		}
		if actual == category {
			result = append(result, bundle)
		}
	}
	return result, nil
}

func availableExactBundleCategories(managed catalog.Catalog, application string) ([]exactBundleCategory, error) {
	allowed := map[string]map[string]bool{
		"comfyui":   {"comfyui-images": true, "comfyui-videos": true, "comfyui-addons": true},
		"llama-cpp": {"llama-cpp": true},
		"dwarfstar": {"dwarfstar": true},
	}
	var filter map[string]bool
	if application != "" {
		var ok bool
		filter, ok = allowed[application]
		if !ok {
			return nil, controlerr.Usage("application %q has no exact-bundle browser", application)
		}
	}
	var result []exactBundleCategory
	for _, category := range exactBundleCategories {
		if filter != nil && !filter[category.ID] {
			continue
		}
		bundles, err := exactBundlesInCategory(managed, category.ID)
		if err != nil {
			return nil, err
		}
		if len(bundles) > 0 {
			result = append(result, category)
		}
	}
	return result, nil
}

func exactBundleDisplayID(bundle catalog.Bundle, category string) string {
	prefix := map[string]string{"llama-cpp": "llama-", "dwarfstar": "dwarfstar-"}[category]
	return strings.TrimPrefix(bundle.ID, prefix)
}

func (app *App) guidedExactBundle(managed catalog.Catalog, application string) (string, error) {
	categories, err := availableExactBundleCategories(managed, application)
	if err != nil {
		return "", err
	}
	if len(categories) == 0 {
		return "", controlerr.New("no exact bundles are available")
	}
	category := categories[0]
	if len(categories) > 1 {
		rows := make([][]string, 0, len(categories))
		for _, candidate := range categories {
			bundles, err := exactBundlesInCategory(managed, candidate.ID)
			if err != nil {
				return "", err
			}
			rows = append(rows, []string{candidate.Description, fmt.Sprintf("%d %s", len(bundles), plural(len(bundles), "bundle", "bundles"))})
		}
		index, err := app.promptMenuColumns("Browse exact bundles:", rows, []ui.Column{{}, {Right: true}}, "bundle category")
		if err != nil {
			return "", err
		}
		category = categories[index]
	}
	bundles, err := exactBundlesInCategory(managed, category.ID)
	if err != nil {
		return "", err
	}
	terminal := app.terminal(app.Stdout)
	rows := make([][]string, 0, len(bundles))
	for _, bundle := range bundles {
		rows = append(rows, []string{terminal.Command(exactBundleDisplayID(bundle, category.ID)), humanSize(managed.BundleSize(bundle)), bundle.Description})
	}
	index, err := app.promptMenuColumns(category.Description+":", rows, []ui.Column{{}, {Right: true}, {}}, "bundle selection")
	if err != nil {
		return "", err
	}
	return bundles[index].ID, nil
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
