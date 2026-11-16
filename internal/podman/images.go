package podman

import (
	"context"
	"encoding/hex"
	"strings"

	"paracetamol/internal/controlerr"
)

// InspectImageID returns a full config digest in archive-compatible form.
// Podman may print .Id without the algorithm prefix; normalize only here so
// generic capture and container identities retain their own protocols.
func (client Client) InspectImageID(ctx context.Context, reference string) (string, error) {
	raw, err := client.Capture(ctx, []string{"image", "inspect", "--format", "{{.Id}}", reference}, "cannot inspect image "+reference)
	if err != nil {
		return "", err
	}
	digest := strings.TrimPrefix(raw, "sha256:")
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 || strings.ToLower(digest) != digest {
		return "", controlerr.New("invalid image ID returned for %s: expected a full SHA-256 config digest", reference)
	}
	return "sha256:" + digest, nil
}
