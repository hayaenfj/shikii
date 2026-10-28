**English** · [日本語](why-shikii.ja.md)

# Why shikii was made, and what was decided (readable version)

2026-10-17. The design record itself is in [`decision-ops-framework.md`](decision-ops-framework.md). That one is written for the author to look back on later, and it is packed with numbers and terms.
This document rewrites the same content for people reading it for the first time.

## 1. What this is about

You ask an AI "yes / no" or "which one?", and it answers with a probability. A program looks at that probability and decides whether to act on its own.
Whenever you try to do this, you trip over the same thing: **"Can I trust that probability?"**

shikii is a tool for answering that question. It does not make the AI itself; it borrows one. What we own is only the flow of "record the probability, check it against the right answer, draw a line above which it can be trusted, and remove the line when it goes stale."

The shape we aim for (set on 2026-10-17): **people give only their intent and the right answers. The way of asking, the trust and the steps are all grown by the machine, and the knowledge that grows is shared among people.** Today's shikii has the trust side done, and the asking side (`tune`, adopting from the screen, the shared wiki) has started to work. The steps (with one model: the smallest question → add context → break it down → a person) are still to come.

## 2. "Deciding AI" has arrived

Apart from AI that writes text (like ChatGPT), in 2026 **AI that only chooses** became widely available. TypeSafe's Jev, Liquid's d1, OpenAI, Cloudflare, Microsoft and others released hosted versions, it became possible to run them on your own machine with llama.cpp, and from OpenRouter they can all be called the same way.

There are only 3 ways to ask.

| Type | What you ask | What comes back |
|---|---|---|
| Yes/no (`noul`) | "Are these 2 bug reports about the same bug?" | The probability of yes |
| Choice (`choice`) | "Is this request a change, a question, a review, an operation, or something else?" | A probability for each option |
| Level (`score`) | "On a scale of 5, how strongly are they related?" | A probability for each level |

Since they generate no text, they are fast and cheap, and since the answer is a number, a program can handle it.

This "get a probability out of an AI" part is already a product. We do not build it.

## 3. The hole everyone escapes with a warning

Every company's manual, llama.cpp and OpenRouter all carry the same warnings.

- "The probabilities are not adjusted to your data"
- "Thresholds cannot be carried over to another model or another question"
- "Confidence is how lopsided the probabilities are, not how correct they are"

They are only written down; nobody has released a tool that deals with this. So each time, the user has to check it themselves, draw the line themselves, and keep watch themselves for the line going stale. This is the hole, and shikii fills only this hole.

## 4. We had built the same thing by hand twice

Before making this tool, we had hand-built the same mechanism twice, in two different places.

- **The Claude Code helper (claude-jev)**: reads a request and asks, with 5 options, "is this a request to change files?"; at 0.75 or above it goes ahead on its own, at 0.6 or above it asks for confirmation, and below that it ignores it
- **Automatic linking of business records (had-scorer, a separate project)**: asks yes/no whether 2 records are about the same matter, adjusts the probability, and then links them on its own at 0.7 or above

The two had completely different uses, yet inside they had the same shape. Decide the question, build the text to show, adjust the probability, draw a line and turn it into an action, control whether it may be sent outside, and remove it when it misses. This sequence is exactly shikii's set of parts.

The second time (had-scorer), 5 things went badly wrong, and these are the grounds for the design.

1. **An adjustment belongs to one question.** When the adjustment made for "are they similar" was reused for the question "are they related", nearly everything scored 0
2. **The question text and the adjustment numbers are a pair.** Change only one, and it breaks quietly, without even an error
3. **Some failures cannot be fixed by adjustment.** After the question text was changed, the lowest score of related pairs fell below the highest score of unrelated pairs. Once that happens, adjustment cannot help. The only way is to put the question text back
4. **Get the AI's conversation format wrong, and it breaks before ability even matters.** A model called Qwen has a habit of starting to think with `<think>` first; that came out before the yes/no words were read, and the answer was leftovers
5. **Whenever you change the model, redo the adjustment.** When a small model's adjustment was applied to a large model, everything became 1.00, and even doubtful pairs were linked on their own

## 5. What we take on, and what we don't

**We take on**

- Defining the question (one file)
- Asking, and turning the answer into three choices (act on its own / ask a person / discard). Controlling which AI to send to, and whether it may be sent
- Recording every decision, with the raw probability
- Picking up right answers from what users do
- Drawing a line from the right answers, for each pair of question and model
- Checking whether a line is still right (failing it in CI)
- Warning about decisions that stopped being right

**We don't take on**

- Our own format for calling AI (we ride on the common format that already exists, as is)
- Training or tuning AI
- The calculation that gets probabilities out of AI itself (left to llama.cpp and the vendors. As an exception, we keep just one small converter that makes an ordinary chat model look like a decision model)
- Evaluating AI that writes text (a different problem)

## 6. What we decided, and why

### A question is one file

The question text, the options, the template of the text shown to the AI, where it may be sent, the position of the line, and "the result of drawing the line" are written in one file.
The first half does not depend on the AI, and the second half (the line) differs for each AI, so they are kept separate inside the same file.

The result of drawing the line is **not something a person writes but something a command writes in**. If you fix it by hand, the check fails.

### What comes back is not a probability but three choices

The program is given only one of `act` (act on its own), `ask` (ask a person), `discard` (discard), or `abstain` (not decided).
We do not let a number like 0.7 be written into the program. That way, moving the line does not require touching the program.

### The "key" of a line is the hash of the question text and the AI's name

What a line is drawn from is the pair of a hash that combines "question text + text template + options" and the name the AI gives for itself.
Change one word of the question and the hash changes; when the AI's version goes up, the name changes. Either way, the line drawn before is no longer used.

This makes failures 1, 2 and 5 from Section 4 **unable to happen, by structure**. Try to reuse it and the key does not match; change only one half and the line disappears; change the model and the line disappears.

The order in which the text is arranged for the AI is also part of the key. With an ordinary chat model, placing the text before or after the options moved the accuracy by 5 points.

**Even when a line disappears, the right answers do not.** Right answers like "this request was a change request" or "these 2 were the same bug" were decided by people, and they depend neither on the wording nor on the AI.
What disappears is only the probability (the number the AI returned at the time), and that can be taken again. So after changing the wording or the AI, `calibrate` asks again about all the texts that are kept, with the new combination, and draws the line again.
A person's job is only to decide the right answer once for each input, and there is no need to decide again each time the wording is fixed (added on 2026-10-17. The text is always kept in the local records, so no conditions are needed).

### Until there is a line, nothing acts on its own

An AI answer that has not been checked against right answers stops at `ask`, however high the probability. We made "do not act automatically on unadjusted probabilities" the default.
This is shikii's most important promise, and the tests also confirm that "deliberately removing the line makes act stop appearing."

### Right answers are picked up without asking users to "please rate this"

| What the user did | Right answer picked up |
|---|---|
| Chose an answer under "ask a person" | That answer is right |
| Later undid something that acted on its own | The AI's answer is wrong |
| Something acted on its own and was not undone for a while | Probably right (counted lightly, as a weak right answer) |
| Redid by hand something that had been discarded | Discarding it was wrong |

Partial right answers of the kind "it was not that one" (`!option`) are also accepted. They count toward the number of mistakes, but are not used to learn the adjustment.

### Adjustments are chosen by comparing them. What they can't fix is fixed with wording

For each type there are 2 ways to adjust: no adjustment (`identity`), and, for yes/no, correcting the probability with 2 numbers (`platt`), or, for choices, correcting each option's habit (`vector`).
Which is better is decided by splitting the right answers into 5 parts and doing "learn on 4, test on 1" 5 times. We choose among those that meet the precision needed for `act`, and if both are the same, we don't adjust. Of course you score better if you practice on the exam questions, so we measure on questions not used for practice.

When we actually tried it, there were clearly **failures that adjustment does not fix**. In guessing the kind of a request, when 19 of 21 mistakes fell into "something else", adjustment could lower "something else", but it would also drag in requests that really are "something else". Rewriting the option descriptions made the mistakes themselves go away (accuracy 0.68 → 0.90).

We made it a rule: **if the mistakes gather on one option, fix the wording before adjusting.** Use adjustment only when "the order is right but confidence is lacking."

One more: if the scores do not tell the right pairs from the wrong ones at all (failure 3 in Section 4; since 0.10 measured as the AUC, whose 95% interval reaches 0.5), it does not give adjustment numbers and says "please change the question text." A few right pairs scoring below a few wrong ones is normal and does not stop it.

### The check fails in CI

`shikii check` looks at whether the line's hash matches the current question text, whether a hand-written line is mixed in, whether accuracy above the line is high enough, and whether the probabilities tell the answers apart at all, and if not, it ends in failure.
It turns the "test that fixes the pair of question text and constants" that was originally in had-scorer into a command that can be used from any language.

### Watch for decline, and remove

`shikii drift` uses recent right answers to see whether things have got worse than when the line was drawn. If they have, it warns.
A decision whose "hits" have dropped should be told "remove it", and we are making automatic what we learned in claude-jev when we removed by hand a decision with 0 hits.

### Whether it may be sent outside is controlled by the tool

During work that touched production data, do not send to outside AI. If there is a local AI, decide with it; if there isn't, answer "not decided". shikii takes this on, not the program that uses it.

## 7. What we learned by actually trying it

- **Whether adjustment is needed was decided by numbers, not by claims.** OpenJev came out as "no adjustment" on both of 2 yes/no questions, for Kev-4B adjustment won slightly, and ordinary chat models got 2 to 4 times better with adjustment
- **Adjustment worked well for ordinary chat models.** When reading a 27B model called Bonsai 2, the quality of the probabilities for detecting duplicate bug reports went 0.049 → 0.019 (smaller is better)
- **Even an ordinary chat model reached the same accuracy as a dedicated model.** Just adding one small converter to the original llama.cpp, with no modification and no retraining. 0.6 seconds per question
- **The order of the text moves accuracy.** Same model, same weights, only the position of the text changed: 0.919 → 0.968. So the order is also part of the key
- **Bias from the order of options was negligible with a dedicated model.** Reordered 100 times, the answer swapped 0 times. We made "measure before fixing" a rule
- **Japanese differs by model.** Kev-4B scored 0.913 on Japanese requests; the 27B model got every question right
- **With models whose probabilities lean to the extremes, the adjustment calculation ran away.** An ordinary model's word readout gives extreme values like 0.999 or 0.003, so a naive calculation method diverged. The test comparison went back to "no adjustment" and prevented an accident, but the calculation method itself was also fixed. Those 32 items are now a test as they are

## 8. What we think is risky

- **A framework fails more easily than a part.** If the shape doesn't fit, nobody uses it. So we first checked whether the 2 existing uses could be rebuilt with this framework (checked)
- **Don't build big.** Nothing else was built until three things worked: the settings file, asking, and drawing the line
- **The market is one month old.** Make our own way of calling, and we'll be left behind by updates to llama.cpp and OpenRouter. Ride thinly on top of the common format
- **Right answers are biased.** If right answers come only from "ask a person", the high-confidence region can't be checked. Weak right answers from undos and the passage of time help, but it is still biased. The distribution of right answers is attached to the result of drawing the line
- **The weight of weak right answers.** "Not undone = right" is lenient. Count it lightly
- **OpenJev cannot be used commercially.** The recommendation is TypeSafe's Jev, and the local default when no key can be set is Ternary-Bonsai-27B (Apache 2.0. Switched from Kev-4B in 0.7.0)

## 9. What we haven't checked yet

- When weak right answers of the "it was not that one" kind reach the hundreds, how many false alarms the decline warning gives
- When more right answers pile up, whether another adjustment method (isotonic regression) beats the current one
- How mature the other OSS that make ordinary models look like decision models (local-typed-decisions and others) are

## 10. More detail

The design record, including numbers and sources, is [`decision-ops-framework.md`](decision-ops-framework.md). How to use it is in [`guide.md`](guide.md). The model comparison table is [`model-comparison.md`](model-comparison.md).
