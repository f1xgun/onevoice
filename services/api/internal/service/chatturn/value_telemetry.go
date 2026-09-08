package chatturn

import "github.com/f1xgun/onevoice/services/api/internal/service/valuetelemetry"

// SetValueTelemetry attaches the server-owned completion sink before serving.
func (t *Turn) SetValueTelemetry(sink valuetelemetry.Sink) { t.valueTelemetry = sink }
