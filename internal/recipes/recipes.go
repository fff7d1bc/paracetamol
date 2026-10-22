// Package recipes owns the intentionally small guided content selections.
package recipes

import (
	"fmt"
	"sort"

	"paracetamol/internal/identity"
)

type Recipe struct {
	ID          string
	Application string
	Description string
	Bundles     []string
	Next        []string
}

func (recipe Recipe) NextCommand() string { return identity.Command(recipe.Next...) }

var byApplication = map[string][]Recipe{
	"comfyui": {
		{ID: "image", Application: "comfyui", Description: "Qwen Image FP8, 4-step Lightning", Bundles: []string{"qwen-image-2512-fp8-lightning"}, Next: []string{"run", "comfyui"}},
		{ID: "edit", Application: "comfyui", Description: "Qwen Image Edit FP8, 4-step Lightning", Bundles: []string{"qwen-image-edit-2511-fp8-lightning"}, Next: []string{"run", "comfyui"}},
		{ID: "t2v", Application: "comfyui", Description: "Wan 2.2 T2V FP8, 4-step Lightning", Bundles: []string{"wan-2.2-t2v-14b-fp8-lightning"}, Next: []string{"run", "comfyui"}},
		{ID: "i2v", Application: "comfyui", Description: "Wan 2.2 I2V FP8, 4-step Lightning", Bundles: []string{"wan-2.2-i2v-14b-fp8-lightning"}, Next: []string{"run", "comfyui"}},
	},
	"llama-cpp": {
		{ID: "qwen3.8", Application: "llama-cpp", Description: "Unsloth Qwen3.8 dense 27B Dynamic Q8_K_XL with MTP", Bundles: []string{"llama-unsloth-qwen3.8-27b-ud-q8-k-xl"}, Next: []string{"run", "llama-cpp", "server", "--preset", "unsloth-qwen3.8-27b-mtp-ud-q8-k-xl"}},
		{ID: "qwen3.8-flash-next", Application: "llama-cpp", Description: "Unsloth Qwen3.8 Flash-Next 125B-A6B Dynamic Q4_K_XL for experimental Strix Halo SSD-backed loading", Bundles: []string{"llama-unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"}, Next: []string{"run", "llama-cpp", "server", "--preset", "unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"}},
		{ID: "muse-glimmer", Application: "llama-cpp", Description: "Meta Muse Glimmer 30B Dynamic Q4_K_XL with DFlash", Bundles: []string{"llama-meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash"}, Next: []string{"run", "llama-cpp", "server", "--preset", "meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"}},
	},
}

func Applications() []string {
	result := make([]string, 0, len(byApplication))
	for application := range byApplication {
		result = append(result, application)
	}
	sort.Strings(result)
	return result
}

func IsApplication(application string) bool {
	_, ok := byApplication[application]
	return ok
}

func ForApplication(application string) ([]Recipe, error) {
	values, ok := byApplication[application]
	if !ok {
		return nil, fmt.Errorf("unknown content application %q", application)
	}
	return append([]Recipe(nil), values...), nil
}

func Find(application, identifier string) (Recipe, error) {
	values, err := ForApplication(application)
	if err != nil {
		return Recipe{}, err
	}
	for _, value := range values {
		if value.ID == identifier {
			return value, nil
		}
	}
	return Recipe{}, fmt.Errorf("unknown %s recipe %q", application, identifier)
}
