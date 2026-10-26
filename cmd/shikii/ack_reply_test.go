package main

import "testing"

// 0.9.3: only a bare acknowledgement is attached to the previous request. Japanese requests are short (most were under the old
// 40-character limit), so a Codex session of 19 requests became one conversation and reflection almost never ran.
// A short request with content starts a new conversation.
func TestOnlyBareAcknowledgementsAreReplies(t *testing.T) {
	for _, s := range []string{"y", "Y", "ok", "OK.", "okay", "yes", "go ahead", "continue", "lgtm", "next",
		"\u306f\u3044", "\u7d9a\u3051\u3066", "\u6b21\u3078", "\u3084\u308a\u307e\u3057\u3087\u3046", "\u3067\u306f\u7d9a\u3051\u307e\u3057\u3087\u3046", "\u304a\u9858\u3044\u3057\u307e\u3059", "\u9032\u3081\u3066", "\u4e86\u89e3\u3002", "\u3044\u3044\u3088"} {
		if !isAckReply(s) {
			t.Fatalf("%q is an acknowledgement and is attached to the previous request", s)
		}
	}
	for _, s := range []string{"and the other one", "fix it", "cold\u3092\u66f4\u306b\u9ad8\u901f\u5316", "\u307e\u3060cold\u524a\u308c\u305d\u3046",
		"\u66f4\u306b\u9ad8\u901f\u5316\u3059\u308b\u4f59\u5730\u306f\u3042\u308a\u307e\u3059\u304b\uff1f", "main\u53d6\u308a\u8fbc\u3093\u3067", "\u3053\u306e\u30d6\u30e9\u30f3\u30c1\u306b\u53d6\u308a\u8fbc\u3093\u3067\u3044\u3044\u3088", "1", "y\u3067\u3082\u3044\u3044\u3051\u3069\u30c6\u30b9\u30c8\u3082\u66f8\u3044\u3066"} {
		if isAckReply(s) {
			t.Fatalf("%q is a request of its own and starts a new conversation", s)
		}
	}
}
