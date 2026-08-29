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
	wantBundles := []string{"llama-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"}
	if !reflect.DeepEqual(recipe.Bundles, wantBundles) {
		t.Fatalf("bundles = %v, want %v", recipe.Bundles, wantBundles)
	}
	wantNext := "./paracetamol run llama-cpp server --preset qwen3.8-flash-next-125b-a6b-ud-q4-k-xl"
	if got := recipe.NextCommand(); got != wantNext {
		t.Fatalf("next command = %q, want %q", got, wantNext)
	}
}
