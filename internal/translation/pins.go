package translation

// DefaultPins carries the verified per-model acquisition pins used by the
// application. Values below were verified from an actual full upstream GET
// plus a second-mirror crosscheck of bytes and SHA-256 (upstream publishes no
// checksums; sizes and digests below are the locally pinned observations).
// Full provenance is documented by the parent outside this package.
//
// The model archives carry no license field; the weights license is
// unresolved, so no license is stated or implied here.
//
// Each entry must stay exact: Title, Version, FileName, SourceURL, TotalBytes
// and ExpectedSize (identical exact byte size, both > 0), and ChecksumValue
// (lowercase hex SHA-256 of the exact artifact bytes). Do not invent or edit
// these values without repeating the verification.
var DefaultPins = map[string]Acquisition{
	ModelTREN: {
		Title:         "Türkçe → İngilizce",
		Version:       "1.5",
		FileName:      "translate-tr_en-1_5.argosmodel",
		SourceURL:     "https://data.argosopentech.com/argospm/v1/translate-tr_en-1_5.argosmodel",
		TotalBytes:    120561223,
		ExpectedSize:  120561223,
		ChecksumValue: "15217905586561fac843efb5730aabff9a1534d5a9aa3f6ebcc0f9641394ac2d",
	},
	ModelENTR: {
		Title:         "İngilizce → Türkçe",
		Version:       "1.5",
		FileName:      "translate-en_tr-1_5.argosmodel",
		SourceURL:     "https://data.argosopentech.com/argospm/v1/translate-en_tr-1_5.argosmodel",
		TotalBytes:    124742526,
		ExpectedSize:  124742526,
		ChecksumValue: "2d553a00880a0c21dea5b1c375535659e6bf18c9bf2ce9847c2a2fd2ff50316a",
	},
}
