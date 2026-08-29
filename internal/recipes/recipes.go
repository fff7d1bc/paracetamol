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
		{ID: "qwen3.6", Application: "llama-cpp", Description: "Qwen3.6 dense 27B MTP Q8_0 and sparse 35B-A3B MTP Dynamic Q8_K_XL", Bundles: []string{"llama-qwen3.6-27b-mtp-q8-0", "llama-qwen3.6-35b-a3b-mtp-ud-q8-k-xl"}, Next: []string{"run", "llama-cpp", "server", "--preset", "qwen3.6-27b-mtp-q8-0"}},
		{ID: "qwen3.8", Application: "llama-cpp", Description: "Qwen3.8 dense 27B Dynamic Q8_K_XL with MTP", Bundles: []string{"llama-qwen3.8-27b-ud-q8-k-xl"}, Next: []string{"run", "llama-cpp", "server", "--preset", "qwen3.8-27b-mtp-ud-q8-k-xl"}},
		{ID: "qwen3.8-flash-next", Application: "llama-cpp", Description: "Qwen3.8 Flash-Next 125B-A6B Dynamic Q4_K_XL for experimental Strix Halo SSD-backed loading", Bundles: []string{"llama-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"}, Next: []string{"run", "llama-cpp", "server", "--preset", "qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"}},
		{ID: "kat-coder", Application: "llama-cpp", Description: "KAT-Coder V2.5 Dev 35B Q8_0 coding and agent model", Bundles: []string{"llama-kat-coder-v2.5-dev-q8-0"}, Next: []string{"run", "llama-cpp", "server", "--preset", "kat-coder-v2.5-dev-q8-0"}},
		{ID: "muse-glimmer", Application: "llama-cpp", Description: "Muse Glimmer 30B Dynamic Q4_K_XL with DFlash", Bundles: []string{"llama-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash"}, Next: []string{"run", "llama-cpp", "server", "--preset", "muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k"}},
		{ID: "translation-hy", Application: "llama-cpp", Description: "Tencent HY-MT1.5 7B Q8_0 multilingual translator", Bundles: []string{"llama-hy-mt1.5-7b-q8-0"}, Next: []string{"run", "llama-cpp", "server", "--preset", "hy-mt1.5-7b-q8-0"}},
		{ID: "translation-gemma", Application: "llama-cpp", Description: "TranslateGemma 27B IT Q8_0 manually prompted translator", Bundles: []string{"llama-translategemma-27b-it-q8-0"}, Next: []string{"run", "llama-cpp", "server", "--preset", "translategemma-27b-it-q8-0"}},
		{ID: "shisa-v2.1", Application: "llama-cpp", Description: "Shisa V2.1 Llama 3.3 70B Q8_0 Japanese/English translator", Bundles: []string{"llama-shisa-v2.1-llama3.3-70b-q8-0"}, Next: []string{"run", "llama-cpp", "server", "--preset", "shisa-v2.1-llama3.3-70b-q8-0"}},
	},
	"dwarfstar": {
		{ID: "flash-0731-q2-imatrix", Application: "dwarfstar", Description: "DeepSeek V4 Flash 0731 chat-v2 imatrix", Bundles: []string{"dwarfstar-deepseek-v4-flash-0731-q2-imatrix"}, Next: []string{"run", "dwarfstar", "server"}},
		{ID: "flash-0731-q2-imatrix-dspark", Application: "dwarfstar", Description: "DeepSeek V4 Flash 0731 Q2 imatrix with opt-in DSpark", Bundles: []string{"dwarfstar-deepseek-v4-flash-0731-q2-imatrix-dspark"}, Next: []string{"run", "dwarfstar", "server", "--dspark"}},
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
