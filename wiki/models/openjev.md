# OpenJev (ggml-org/OpenJev-GGUF, 27B, CC BY-NC 4.0)

- A trained decision model. A cut above in accuracy: prompt-intent top-1 0.984, Japanese 1.000
- Raw probabilities suffice for noul (same-issue 0.007, same-inquiry 0.037, both identity). Platt does not improve in CV
- Slow: 3.8 seconds per question, 16 GB resident. Suited to batches; heavy for a single question in a hook
- Non-commercial. Cannot be the local default

Evidence: [docs/model-comparison.md](https://github.com/hayaenfj/shikii/blob/main/docs/model-comparison.md)
