# Ternary-Bonsai-27B Q2_g64 (PrismML; an ordinary chat model that runs on upstream llama.cpp 0.7.0, read with `shikii-readout`)

- Not a decision model. The options get A, B, …, a single token is run, and the letter probabilities are read (readout)
- **Where the text sits moves the ranking**: with the state before the options, top-1 0.919 / Japanese 0.870; right before the answer (state-last), 0.968 / 1.000. Same weights → [state-last-for-recurrent-readout](../patterns/state-last-for-recurrent-readout.md)
- Its base is the Qwen3.5/3.8 family (with recurrent layers), so `cache_prompt` does not help without a checkpoint at the end of the prefix. The adapter sends the prefix first. 0.58 seconds per question
- Letter readout pushes probabilities to the extremes (0.999 / 0.003). Platt helps on noul (same-issue 0.048 → 0.024, same-inquiry 0.033 → 0.020). prompt-intent (choice) is raw precision 1.000 / recall 0.983, so identity
- Good at a 5-level score (same bug P(≥3) 0.950, unrelated 0.008)
- Commercial use allowed (Apache 2.0), no fork needed. In this wiki, the model closest to "a default for any use"

Evidence: [integrations/readout.md](https://github.com/hayaenfj/shikii/blob/main/integrations/readout.md), [docs/model-comparison.md](https://github.com/hayaenfj/shikii/blob/main/docs/model-comparison.md)
