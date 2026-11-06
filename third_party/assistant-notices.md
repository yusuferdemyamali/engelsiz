# Optional local AI components

Engelsiz 1.0.0 does not bundle or silently download AI model weights. The user explicitly requests each model and the CPU inference runtime. These components retain their own licenses, separate from Engelsiz's MIT license.

| Component | Distributor | License | Source |
|---|---|---|---|
| Qwen3-0.6B Q4_K_M | Unsloth community quantization of Qwen3 | Apache-2.0 | https://huggingface.co/unsloth/Qwen3-0.6B-GGUF |
| Qwen3-1.7B Q4_K_M | Unsloth community quantization of Qwen3 | Apache-2.0 | https://huggingface.co/unsloth/Qwen3-1.7B-GGUF |
| Qwen3-4B Q4_K_M | Qwen | Apache-2.0 | https://huggingface.co/Qwen/Qwen3-4B-GGUF |
| llama.cpp b12174 | ggml-org | MIT | https://github.com/ggml-org/llama.cpp/tree/18ab62a8ec5144ae5d2f65e78374ba69652d75dd |

Model repository `LICENSE` files contain Apache License 2.0. The smaller Q4 files are not official Qwen quantizations; their exact repository revisions, LFS hashes and byte counts are recorded in [offline-ai.md](../docs/offline-ai.md) and the application catalog.

llama.cpp upstream license: https://github.com/ggml-org/llama.cpp/blob/18ab62a8ec5144ae5d2f65e78374ba69652d75dd/LICENSE

The optional runtime archive is fetched directly from the pinned upstream GitHub release and verified against its exact SHA-256. Keep the archive's companion libraries and license files together. The upstream runtime build can include additional components (for example BoringSSL and OpenMP); this short notice is not an assertion that a new redistribution of the runtime is legally complete. Before bundling or republishing runtime binaries in an Engelsiz installer, audit the actual archive's linked dependencies and retain their original notices. This implementation's installer does not bundle these AI binaries.

Downloaded knowledge sources retain their own licenses and attribution. Source snippets in AI context do not acquire the model's or the application's license. AI responses are generated text, not verified quotations or medical advice.
