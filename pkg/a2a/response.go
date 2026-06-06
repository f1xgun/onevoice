package a2a

// OK builds a successful ToolResponse for req carrying result.
func OK(req ToolRequest, result map[string]any) *ToolResponse {
	return &ToolResponse{TaskID: req.TaskID, Success: true, Result: result}
}
