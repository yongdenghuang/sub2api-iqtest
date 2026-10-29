# Embedded SVG renderer

`resvg.wasm` is the unchanged artifact from [boolean-maybe/go-resvg v0.0.2](https://github.com/boolean-maybe/go-resvg/tree/v0.0.2), Apache-2.0 ([license](../../../resources/licenses/svgrender/LICENSE.resvg-wasm)). Its source and reproducible Rust/Binaryen build instructions are in that tagged repository (`rust/`, `Dockerfile`, `Makefile`). It embeds resvg and DejaVu font subsets; their licenses and the complete locked Rust dependency notices are shipped in [resources/licenses/svgrender/THIRD_PARTY_NOTICES.txt](../../../resources/licenses/svgrender/THIRD_PARTY_NOTICES.txt), which is included in the container and native release.

SHA-256: `ba867ed3f0ebee782d3575ca52891f3206198b2741a88329b9a080857aeb1972`

This application's Go adapter is independent: no host filesystem/network is exposed; each render has a 256 MiB WASM memory limit, a 20-second cancellation deadline, bounded canvas dimensions, and a single-render concurrency limit. The PNG is composited on white and passed as actual image input to the reviewer.
