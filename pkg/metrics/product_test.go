package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestIncChatTurn(t *testing.T) {
	for _, outcome := range []string{"done", "error", "pause_hitl", "rejoined_resume"} {
		t.Run(outcome, func(t *testing.T) {
			before := testutil.ToFloat64(chatTurnsTotal.WithLabelValues(outcome))
			IncChatTurn(outcome)
			after := testutil.ToFloat64(chatTurnsTotal.WithLabelValues(outcome))
			require.InDelta(t, before+1, after, 0.0001)
		})
	}
}

func TestIncChatTurn_OnlyIncrementsSingleLabel(t *testing.T) {
	beforeErr := testutil.ToFloat64(chatTurnsTotal.WithLabelValues("error"))
	IncChatTurn("done")
	afterErr := testutil.ToFloat64(chatTurnsTotal.WithLabelValues("error"))
	require.InDelta(t, beforeErr, afterErr, 0.0001, "incrementing 'done' must not affect 'error'")
}

func TestIncHITLDecision(t *testing.T) {
	for _, decision := range []string{"approve", "edit", "reject"} {
		t.Run(decision, func(t *testing.T) {
			before := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues(decision))
			IncHITLDecision(decision)
			after := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues(decision))
			require.InDelta(t, before+1, after, 0.0001)
		})
	}
}

func TestIncHITLDecision_OnlyIncrementsSingleLabel(t *testing.T) {
	beforeReject := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues("reject"))
	IncHITLDecision("approve")
	afterReject := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues("reject"))
	require.InDelta(t, beforeReject, afterReject, 0.0001, "incrementing 'approve' must not affect 'reject'")
}

// TestIncHITLDecision_CollapsesUnknown guards the cardinality bound: an
// unvalidated upstream action string must land in the bounded "other"
// bucket and must NOT create a new series for the raw value.
func TestIncHITLDecision_CollapsesUnknown(t *testing.T) {
	beforeOther := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues(labelOther))
	IncHITLDecision("frobnicate")
	IncHITLDecision("")
	afterOther := testutil.ToFloat64(hitlDecisionsTotal.WithLabelValues(labelOther))
	require.InDelta(t, beforeOther+2, afterOther, 0.0001, "unknown decisions must increment 'other'")

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	mf := findMetric(families, "hitl_decisions_total")
	require.NotNil(t, mf)
	for _, m := range mf.GetMetric() {
		for _, l := range m.GetLabel() {
			require.NotContains(t, []string{"frobnicate", ""}, l.GetValue(),
				"raw unknown decision values must never become a series")
		}
	}
}

func TestIncPostsPublished(t *testing.T) {
	for _, platform := range []string{"telegram", "vk", "yandex_business", "google_business"} {
		for _, result := range []string{"published", "scheduled", "error"} {
			t.Run(platform+"_"+result, func(t *testing.T) {
				before := testutil.ToFloat64(postsPublishedTotal.WithLabelValues(platform, result))
				IncPostsPublished(platform, result)
				after := testutil.ToFloat64(postsPublishedTotal.WithLabelValues(platform, result))
				require.InDelta(t, before+1, after, 0.0001)
			})
		}
	}
}

func TestIncPostsPublished_OnlyIncrementsSingleLabel(t *testing.T) {
	beforeOther := testutil.ToFloat64(postsPublishedTotal.WithLabelValues("vk", "error"))
	IncPostsPublished("telegram", "published")
	afterOther := testutil.ToFloat64(postsPublishedTotal.WithLabelValues("vk", "error"))
	require.InDelta(t, beforeOther, afterOther, 0.0001,
		"incrementing {telegram,published} must not affect {vk,error}")
}

// TestIncPostsPublished_CollapsesUnknown guards the cardinality bound: a hostile
// or unexpected platform/result must land in the bounded "other" bucket and must
// NOT create a new series for the raw value.
func TestIncPostsPublished_CollapsesUnknown(t *testing.T) {
	beforePlatform := testutil.ToFloat64(postsPublishedTotal.WithLabelValues(labelOther, "published"))
	IncPostsPublished("'; DROP TABLE posts; --", "published")
	IncPostsPublished("", "published")
	afterPlatform := testutil.ToFloat64(postsPublishedTotal.WithLabelValues(labelOther, "published"))
	require.InDelta(t, beforePlatform+2, afterPlatform, 0.0001,
		"unknown platforms must collapse to 'other'")

	beforeResult := testutil.ToFloat64(postsPublishedTotal.WithLabelValues("telegram", labelOther))
	IncPostsPublished("telegram", "frobnicate")
	IncPostsPublished("telegram", "")
	afterResult := testutil.ToFloat64(postsPublishedTotal.WithLabelValues("telegram", labelOther))
	require.InDelta(t, beforeResult+2, afterResult, 0.0001,
		"unknown results must collapse to 'other'")

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	mf := findMetric(families, "posts_published_total")
	require.NotNil(t, mf)
	for _, m := range mf.GetMetric() {
		for _, l := range m.GetLabel() {
			require.NotContains(t, []string{"'; DROP TABLE posts; --", "frobnicate"}, l.GetValue(),
				"raw unknown label values must never become a series")
		}
	}
}

func TestIncReviewsReplied(t *testing.T) {
	for _, platform := range []string{"telegram", "vk", "yandex_business", "google_business"} {
		for _, result := range []string{"replied", "pending", "error"} {
			t.Run(platform+"_"+result, func(t *testing.T) {
				before := testutil.ToFloat64(reviewsRepliedTotal.WithLabelValues(platform, result))
				IncReviewsReplied(platform, result)
				after := testutil.ToFloat64(reviewsRepliedTotal.WithLabelValues(platform, result))
				require.InDelta(t, before+1, after, 0.0001)
			})
		}
	}
}

func TestIncReviewsReplied_OnlyIncrementsSingleLabel(t *testing.T) {
	beforeOther := testutil.ToFloat64(reviewsRepliedTotal.WithLabelValues("vk", "error"))
	IncReviewsReplied("yandex_business", "replied")
	afterOther := testutil.ToFloat64(reviewsRepliedTotal.WithLabelValues("vk", "error"))
	require.InDelta(t, beforeOther, afterOther, 0.0001,
		"incrementing {yandex_business,replied} must not affect {vk,error}")
}

// TestIncReviewsReplied_CollapsesUnknown guards the cardinality bound: a hostile
// or unexpected platform/result must land in the bounded "other" bucket and must
// NOT create a new series for the raw value.
func TestIncReviewsReplied_CollapsesUnknown(t *testing.T) {
	beforePlatform := testutil.ToFloat64(reviewsRepliedTotal.WithLabelValues(labelOther, "replied"))
	IncReviewsReplied("\n\t evil", "replied")
	IncReviewsReplied("", "replied")
	afterPlatform := testutil.ToFloat64(reviewsRepliedTotal.WithLabelValues(labelOther, "replied"))
	require.InDelta(t, beforePlatform+2, afterPlatform, 0.0001,
		"unknown platforms must collapse to 'other'")

	beforeResult := testutil.ToFloat64(reviewsRepliedTotal.WithLabelValues("vk", labelOther))
	IncReviewsReplied("vk", "exploded")
	IncReviewsReplied("vk", "")
	afterResult := testutil.ToFloat64(reviewsRepliedTotal.WithLabelValues("vk", labelOther))
	require.InDelta(t, beforeResult+2, afterResult, 0.0001,
		"unknown results must collapse to 'other'")

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	mf := findMetric(families, "reviews_replied_total")
	require.NotNil(t, mf)
	for _, m := range mf.GetMetric() {
		for _, l := range m.GetLabel() {
			require.NotContains(t, []string{"\n\t evil", "exploded"}, l.GetValue(),
				"raw unknown label values must never become a series")
		}
	}
}

func TestProductMetricsLabelShape(t *testing.T) {
	IncChatTurn("done")
	IncHITLDecision("approve")
	IncPostsPublished("telegram", "published")
	IncReviewsReplied("telegram", "replied")

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, tc := range []struct {
		metric string
		labels []string
	}{
		{"chat_turns_total", []string{"outcome"}},
		{"hitl_decisions_total", []string{"decision"}},
		{"posts_published_total", []string{"platform", "result"}},
		{"reviews_replied_total", []string{"platform", "result"}},
	} {
		mf := findMetric(families, tc.metric)
		require.NotNil(t, mf, "%s metric family not found", tc.metric)
		for _, m := range mf.GetMetric() {
			labels := m.GetLabel()
			require.Len(t, labels, len(tc.labels), "%s should have exactly %d label(s)", tc.metric, len(tc.labels))
			for i, l := range labels {
				require.Equal(t, tc.labels[i], l.GetName())
			}
		}
	}
}
