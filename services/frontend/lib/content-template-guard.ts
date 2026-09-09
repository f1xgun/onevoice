interface RenderCompletionState {
  mounted: boolean;
  requestedBusinessId: string;
  currentBusinessId: string | null;
  requestedConversationKey: string;
  currentConversationKey: string;
  inputBeforeRequest: string;
  currentInput: string;
  disabled: boolean;
}

export function shouldApplyRenderedTemplate(state: RenderCompletionState): boolean {
  return (
    state.mounted &&
    !state.disabled &&
    state.requestedBusinessId === state.currentBusinessId &&
    state.requestedConversationKey === state.currentConversationKey &&
    state.inputBeforeRequest === state.currentInput
  );
}
