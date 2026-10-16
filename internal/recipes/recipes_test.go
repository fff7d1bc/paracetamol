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

func TestQwen38RecipeKeepsDenseSelection(t *testing.T) {
	qwen, err := Find("llama-cpp", "qwen3.8")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(qwen.Bundles, []string{"llama-unsloth-qwen3.8-27b-ud-q8-k-xl"}) {
		t.Fatalf("Qwen dense bundles = %v", qwen.Bundles)
	}
}

func TestQwen36RecipeIsRetired(t *testing.T) {
	if _, err := Find("llama-cpp", "qwen3.6"); err == nil {
		t.Fatal("retired recipe remains selectable")
	}
}
