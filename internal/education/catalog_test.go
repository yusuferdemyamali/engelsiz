package education_test

import (
	"encoding/json"
	"strings"
	"testing"

	"engelsiz/internal/education"
)

// TestEmbeddedCatalogPins guards the parent-owned catalog.json: exact
// integrity fields (filename, sizeBytes, sha256) for all eight downloadable
// pins, camel-case wire shape, the three Turkish PhET sim versions with
// explicit noncommercial restrictions, and reference-only entries.
func TestEmbeddedCatalogPins(t *testing.T) {
	res := education.DefaultResources
	if len(res) == 0 {
		t.Fatal("default manifest must be nonempty")
	}
	var downloadable, reference int
	byID := map[string]education.Resource{}
	for _, r := range res {
		byID[r.ID] = r
		if r.Downloadable {
			downloadable++
			if !r.License.Verified {
				t.Fatalf("%s downloadable without verified license", r.ID)
			}
			if !r.VerifiedPin() {
				t.Fatalf("%s downloadable without complete verified pin", r.ID)
			}
		} else {
			reference++
			if r.License.Verified {
				t.Fatalf("%s reference-only with verified license", r.ID)
			}
		}
	}
	// Totals stay parent-owned (pins rotate as sources are re-verified), so
	// only the breakdown shape is asserted, never exact counts.
	if downloadable == 0 || reference == 0 {
		t.Fatalf("catalog must carry both downloads and references, got %d/%d", downloadable, reference)
	}
	type pin struct {
		format, filename, url string
		size                  int64
		sha                   string
	}
	// Exact guards cover only the stable researched core. Water/hygiene pins
	// stay parent-owned and fluid while sources are re-verified; they are
	// covered by the structural VerifiedPin check above, never by exact values.
	want := map[string]pin{
		"who-ky-plus-tr":      {"pdf", "who-ky-plus-tr-2023.pdf", "https://iris.who.int/server/api/core/bitstreams/a879035d-efc5-48c8-a641-a3c7dc0e5a39/content", 5262381, "412f15324f89aa5d1cae4f90bf4d51e2d2e8c08a9ec4ddb15dfc78d74ba56cdb"},
		"wikibooks-tr":        {"zim", "wikibooks_tr_all_maxi_2026-09.zim", "https://download.kiwix.org/zim/wikibooks/wikibooks_tr_all_maxi_2026-09.zim", 26888953, "8a7b54984abb171aace56514bd4a0338e301e5143d011a3666f1170f45893764"},
		"openstax-anatomy-1e": {"pdf", "openstax-anatomy-physiology-1e.pdf", "https://assets.openstax.org/oscms-prodcms/media/documents/AnatomyandPhysiology-OP.pdf", 135405076, "16cb34457cbaa66cc8a4ddd70e745dba0611d6f942bebe51d505ed6e7bd80362"},
		"phet-energy":         {"html", "phet-energy-forms-and-changes-tr-1.4.26.html", "https://phet.colorado.edu/sims/html/energy-forms-and-changes/1.4.26/energy-forms-and-changes_tr.html", 3524853, "d7651fbeb77205f8d0b9fb2421c92e6751ad32e68ec3251b27822b227e4aec5e"},
		"phet-molecule":       {"html", "phet-build-a-molecule-tr-1.0.27.html", "https://phet.colorado.edu/sims/html/build-a-molecule/1.0.27/build-a-molecule_tr.html", 8042999, "bfbe882122a543a905ffc1e3c1d8189797ceb6549b8db7020008e90e64738307"},
		"phet-circuit-dc":     {"html", "phet-circuit-construction-kit-dc-tr-1.5.4.html", "https://phet.colorado.edu/sims/html/circuit-construction-kit-dc/1.5.4/circuit-construction-kit-dc_tr.html", 3509674, "f9ced61a6df0359db38bea10d3bca85c482ea01294e661ab7e2d14e8d61b4503"},
	}
	for id, p := range want {
		r, ok := byID[id]
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if r.Format != p.format || r.Filename != p.filename || r.DownloadURL != p.url {
			t.Fatalf("%s identity %+v", id, r)
		}
		if r.SizeBytes == nil || *r.SizeBytes != p.size {
			t.Fatalf("%s size %+v, want %d", id, r.SizeBytes, p.size)
		}
		if r.SHA256 != p.sha {
			t.Fatalf("%s sha %q", id, r.SHA256)
		}
	}
	// Wire shape stays camel-case per contract.
	raw, err := json.Marshal(byID["phet-energy"])
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"sizeBytes":`, `"sha256":`, `"downloadUrl":`, `"downloadable":`, `"sourceUrl":`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("wire key missing %s in %s", key, raw)
		}
	}
	if strings.Contains(string(raw), "size_bytes") || strings.Contains(string(raw), "download_url") {
		t.Fatalf("snake_case leak in %s", raw)
	}
	phetVersions := map[string]string{
		"phet-energy":     "1.4.26",
		"phet-molecule":   "1.0.27",
		"phet-circuit-dc": "1.5.4",
	}
	for id, version := range phetVersions {
		r, ok := byID[id]
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if r.Format != "html" || r.Language != "tr" || !r.Downloadable || !r.License.Verified {
			t.Fatalf("%s not downloadable: %+v", id, r)
		}
		if r.Version != version {
			t.Fatalf("%s version %q, want %q", id, r.Version, version)
		}
		if r.License.Name != "CC BY-NC 4.0" {
			t.Fatalf("%s license %q", id, r.License.Name)
		}
		if !strings.Contains(r.Restriction, "ticari olmayan") {
			t.Fatalf("%s restriction must be explicit: %q", id, r.Restriction)
		}
		if !strings.HasPrefix(r.DownloadURL, "https://phet.colorado.edu/sims/html/") || !strings.HasSuffix(r.Filename, ".html") {
			t.Fatalf("%s pin %+v", id, r)
		}
	}
	who, ok := byID["who-ky-plus-tr"]
	if !ok || who.License.Name != "CC BY-NC-SA 3.0 IGO" || !who.License.Verified {
		t.Fatalf("who license %+v", who.License)
	}
	for _, id := range []string{"afad-library", "moh-first-aid", "ifixit-repair", "meb-textbooks", "cdc-emergency-water", "cdc-use-safe-water"} {
		r, ok := byID[id]
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if r.Downloadable || r.License.Verified {
			t.Fatalf("%s must stay reference-only: %+v", id, r)
		}
	}
}
