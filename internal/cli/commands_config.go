package cli

import (
	"fmt"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
)

func (app *App) commandConfig(args []string) error {
	if len(args) == 0 || groupHelpRequested(args) {
		app.writeGroupHelp(usage("config", "COMMAND"), [2]string{"init", "create a complete runnable configuration"})
		if len(args) == 0 {
			return controlerr.Usage("choose a config command")
		}
		return nil
	}
	switch args[0] {
	case "init":
		return app.configInit(args[1:])
	default:
		return controlerr.Usage("unknown config command %q", args[0])
	}
}

func (app *App) configInit(args []string) error {
	set := app.flags("config init", usage("config", "init", "[OPTIONS]"))
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if len(set.Args()) > 0 {
		return controlerr.Usage("config init does not accept positional arguments")
	}
	if app.ConfigSelection.Disabled {
		return controlerr.Usage("config init cannot be combined with --no-config")
	}
	path, available, _, err := config.ResolveFile(app.Environment, app.ConfigSelection)
	if err != nil {
		return err
	}
	if !available {
		return controlerr.New("cannot choose a configuration path because HOME and XDG_CONFIG_HOME are unset")
	}
	dataDir, err := config.DefaultDataDirWithConfiguration(app.Environment, config.Configuration{})
	if err != nil {
		return err
	}
	if err := atomicfile.Write(path, config.DefaultContents(dataDir), 0o600, atomicfile.Create); err != nil {
		return err
	}
	terminal := app.terminal(app.Stdout)
	fmt.Fprintf(app.Stdout, "%s %s\n", terminal.Success("Configuration created:"), path)
	fmt.Fprintln(app.Stdout, "All selections are optional; the generated values preserve automatic discovery.")
	return nil
}
