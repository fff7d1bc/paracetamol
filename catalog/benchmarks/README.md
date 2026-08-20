# Benchmark workflow resources

The JSON files in this directory are ComfyUI API-format graphs mechanically
exported from Paracetamol's rendered, curated workflows by the ComfyUI 0.28.0
frontend. Their upstream workflow sources and rendered hashes are pinned in
`../catalog.json`.

The upstream workflow templates are MIT licensed by Comfy Org. Paracetamol
changes model filenames, removes undeclared sample media and optional model
dependencies, and injects deterministic seeds, input media, and output prefixes
at benchmark runtime.

These resources contain no model weights or sample media.
