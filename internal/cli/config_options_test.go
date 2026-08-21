package cli

import (
	"reflect"
	"testing"
)

func TestExtractConfigurationSelectionAcceptsEitherPosition(t *testing.T) {
	for name, arguments := range map[string][]string{
		"before": {"-c", "configs/aion.toml", "run", "gateway"},
		"after":  {"run", "gateway", "--config=configs/aion.toml"},
	} {
		t.Run(name, func(t *testing.T) {
			remaining, selection, err := extractConfigurationSelection(arguments)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(remaining, []string{"run", "gateway"}) || selection.Path != "configs/aion.toml" || selection.Disabled {
				t.Fatalf("remaining=%v selection=%#v", remaining, selection)
			}
		})
	}
}

func TestExtractConfigurationSelectionPreservesPassthrough(t *testing.T) {
	arguments := []string{"agent", "run", "pi", "--", "-c", "model-config.toml"}
	remaining, selection, err := extractConfigurationSelection(arguments)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(remaining, arguments) || selection.Path != "" || selection.Disabled {
		t.Fatalf("remaining=%v selection=%#v", remaining, selection)
	}
}

func TestExtractConfigurationSelectionRejectsConflicts(t *testing.T) {
	if _, _, err := extractConfigurationSelection([]string{"run", "gateway", "-c", "one.toml", "--no-config"}); err == nil {
		t.Fatal("conflicting selection unexpectedly accepted")
	}
}
