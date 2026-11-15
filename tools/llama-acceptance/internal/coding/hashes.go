package coding

// Deliberate fixture changes require a new identity/provenance review, never
// automatic regeneration in Prepare or a candidate acceptance run.
var frozenHashes = map[string]string{
	"vectors/go-merge-unique-v1/input":    "483cf45cff92587af04a840c77417989fd966fddef99692cbadb23e5d460c3db",
	"vectors/go-merge-unique-v1/expected": "73dab6e90ec34d9c3769a368224851f60c05fd90cf2dae4faed7fcee5564afee",
	"vectors/python-coalesce-v1/input":    "a0825e76073e20832273a55a0e46f09609aa120927094c017e88544bbd472d02",
	"vectors/python-coalesce-v1/expected": "a4b9d31169d5159d7cf68601d5bb787a5bff3f7e95468e7203acbdaacbc2efea",
	"fixtures/Containerfile":              "57a3739ca3b78b4344a5a3a902ed316e167044d8af3d9a9c34ac7084cb40eec4",
	"fixtures/go-harness.txt":             "3c8bbcd7460713846e9448ab0ac2144b5678eac0572a9593d4526a9821d32a5e",
	"fixtures/go-prompt.txt":              "5de4e6e015218091f71b85f581e315120c030a350141a30d2fc1f0e83a47e2ca",
	"fixtures/go-solution.txt":            "e10e0b0f83163e3038ef52742848f043c17bdc4ba645c4e43ee9eb468ca990cc",
	"fixtures/python-harness.txt":         "ae68f89d6fb84c3d4f6342c116a2f64c1439246c8a8e95c365759639fd2951a1",
	"fixtures/python-prompt.txt":          "a5acb190bceeed2fdee34f2b26efa2a345b2abbe7e339481f49ea9f813ba38cd",
	"fixtures/python-solution.txt":        "76202bde5eee7686086b59418506fe33d7d7e29202d35f83ee9b873b034c346a",
	"PROVENANCE.md":                       "f04e87f99b9fe4b59ef1e8ee9c77eb8b68749493f51e6fb809652b3ad1e2aa70",
}
