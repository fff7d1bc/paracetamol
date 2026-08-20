// Package identity owns product names and persistent namespace identities.
package identity

// CommandName and DisplayName are variables so the build can select the final
// product name without scattering it through the Go control plane. Persistent
// namespaces remain explicit: changing one requires a deliberate state and
// container migration rather than an innocuous binary rename.
var (
	CommandName = "rocmplete"
	DisplayName = "ROCmplete"
)

const (
	StateNamespace = "rocmplete"
	EnvPrefix      = "ROCMLETE"
	ImageNamespace = "localhost/rocmplete"
	LabelNamespace = "io.github.fff7d1bc.rocmplete"
	Version        = "0.1.0-dev"
)
