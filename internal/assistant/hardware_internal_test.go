package assistant

import "testing"

func verifiedTestPins() map[string]Acquisition {
	mk := func() Acquisition {
		return Acquisition{
			SourceURL: "https://example.invalid/x.gguf", FileName: "x.gguf",
			Version: "v", TotalBytes: 1, ExpectedSize: 1,
			ChecksumValue: "aa", ModelName: "m", ContextSize: 1,
		}
	}
	return map[string]Acquisition{ProfileLite: mk(), ProfileBalanced: mk(), ProfileQuality: mk()}
}

func TestRecommendThresholds(t *testing.T) {
	const gb = int64(1) << 30
	pins := verifiedTestPins()
	defs := Definitions(pins)
	for name, tc := range map[string]struct {
		hw   Hardware
		want string
	}{
		"low-4gb":        {Hardware{TotalMemoryBytes: 4 * gb}, ProfileLite},
		"below-8gb":      {Hardware{TotalMemoryBytes: 6 * gb, AvailableMemoryBytes: 6 * gb}, ProfileLite},
		"balanced-8gb":   {Hardware{TotalMemoryBytes: 8 * gb, AvailableMemoryBytes: 2 * gb}, ProfileBalanced},
		"twelve-not-top": {Hardware{TotalMemoryBytes: 12 * gb}, ProfileBalanced},
		"quality-16gb":   {Hardware{TotalMemoryBytes: 16 * gb}, ProfileQuality},
		"quality-32gb":   {Hardware{TotalMemoryBytes: 32 * gb}, ProfileQuality},
		"unknown":        {Hardware{}, ProfileLite},
	} {
		if got := recommendFor(tc.hw, defs); got != tc.want {
			t.Fatalf("%s: got %s want %s", name, got, tc.want)
		}
	}
	// Unverified richer profiles never win: fall through to Lite.
	bare := map[string]Acquisition{ProfileLite: verifiedTestPins()[ProfileLite]}
	if got := recommendFor(Hardware{TotalMemoryBytes: 64 * gb}, Definitions(bare)); got != ProfileLite {
		t.Fatalf("unverified fallback: got %s", got)
	}
}
