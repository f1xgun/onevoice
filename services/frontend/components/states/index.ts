// components/states/index.ts — Linen empty + loading state catalogue.
//
// Mock spec: design_handoff_onevoice 2/mocks/mock-states.jsx
// (empty + loading sections). Per the loading-states header rule
// "Никакого shimmer и spinner-овражей" — every skeleton in this
// barrel is structurally shaped + statically painted.

export { EmptyFrame } from './EmptyFrame';
export { EmptyInbox } from './EmptyInbox';
export { EmptyChannels } from './EmptyChannels';
export { EmptySearch } from './EmptySearch';
export { EmptyReviews } from './EmptyReviews';
export type { ReviewsEmptyMode } from './EmptyReviews';
export { EmptyTasks } from './EmptyTasks';
export { InlineEmpty, InlineEmptySection } from './InlineEmpty';

export { SkeletonInbox } from './SkeletonInbox';
export { SkeletonMetricStrip } from './SkeletonMetricStrip';
export { SkeletonChat } from './SkeletonChat';
export { SkeletonChannels } from './SkeletonChannels';

export { AIWritingProgress } from './AIWritingProgress';
export { ChannelConnectProgress } from './ChannelConnectProgress';
export type { ChannelConnectStep, ChannelConnectStepState } from './ChannelConnectProgress';
export { InlineSyncPill } from './InlineSyncPill';
