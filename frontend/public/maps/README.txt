Local map rendering assets

The Noto Sans glyph PBFs and dark v4 sprites are copied from
https://github.com/protomaps/basemaps-assets at commit
028c18f713baecad011301ff7a69acc39bcc2ae7. They are served locally and are
not downloaded when the application runs.

The glyph typefaces are covered by fonts/OFL.txt. Mapzen-origin artwork in
the sprites derives from Tangram Icons. sprites/LICENSE.txt is a byte-for-byte
copy of LICENSE.md from https://github.com/tangrams/icons at commit
92510779634f4a006c61ea70e50cb8c52c765a81 (SHA-256:
46d0ca73c10d7366ef7bf3932d8508267096393ccc9ef3a41d1b1d1fe37023f1).

The atlas also contains later Protomaps additions; the Mapzen MIT notice must
not be read as a per-symbol license statement for those additions. The pinned
basemaps-assets tree has no dedicated sprite license file. Protomaps basemap
code, visual-design and original third-party notices are retained through the
separately licensed @protomaps/basemaps dependency in the bundled frontend
notices (LICENSES/runtime-corresponding-source/application/frontend.txt).
Readable copies of this provenance, the font OFL and the icon MIT notice are
also included under LICENSES/map-assets in both distribution packages.
