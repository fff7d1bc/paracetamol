package recipes

import (
	"reflect"
	"testing"
)

func TestFlashNextRecipeUsesAcceptedQ4KXLVariant(t *testing.T) {
	recipe, err := Find("llama-cpp", "qwen3.8-flash-next")
	if err != nil {
		t.Fatal(err)
	}
	wantBundles := []string{"llama-unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"}
	if !reflect.DeepEqual(recipe.Bundles, wantBundles) {
		t.Fatalf("bundles = %v, want %v", recipe.Bundles, wantBundles)
	}
	wantNext := "./paracetamol run llama-cpp server --preset unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"
	if got := recipe.NextCommand(); got != wantNext {
		t.Fatalf("next command = %q, want %q", got, wantNext)
	}
}

func TestSwiftRecipeDoesNotChangeQwenDefault(t *testing.T) {
	swift, err := Find("llama-cpp", "swift1.5")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(swift.Bundles, []string{"llama-ukisai-swift1.5-qwen3.8-27b-mtp-q8-0"}) {
		t.Fatalf("Swift bundles = %v", swift.Bundles)
	}
	if got := swift.NextCommand(); got != "./paracetamol run llama-cpp server --preset ukisai-swift1.5-qwen3.8-27b-mtp-q8-0" {
		t.Fatalf("Swift next command = %q", got)
	}
	qwen, err := Find("llama-cpp", "qwen3.8")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(qwen.Bundles, []string{"llama-unsloth-qwen3.8-27b-ud-q8-k-xl"}) {
		t.Fatalf("Qwen default bundles = %v", qwen.Bundles)
	}
}
