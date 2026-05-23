package wire

import (
	"log/slog"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// Tools constructs the live tool registry, dials NATS, and registers every
// MVP platform tool. NATS unreachable is non-fatal: the registry is returned
// with no platform tools registered (so the orchestrator boots and serves
// `/health/ready`), and the returned *natslib.Conn is nil — cmd/main.go
// branches on nil to skip the NATS health check and the deferred Drain().
//
// No ctx parameter: the v1 nats.go API does not accept a context for dial,
// so threading one through would be ceremonial. Re-add when the dial path
// becomes context-aware.
func Tools(log *slog.Logger, cfg *config.Config) (*toolregistry.Registry, *natslib.Conn, error) {
	reg := toolregistry.NewRegistry()

	nc, err := natslib.Connect(cfg.NATSUrl)
	if err != nil {
		log.Warn("NATS unavailable — tools will return stubs", "url", cfg.NATSUrl, "error", err)
		return reg, nil, nil
	}
	log.Info("connected to NATS", "url", cfg.NATSUrl)
	RegisterPlatformTools(reg, nc)
	return reg, nc, nil
}

// RegisterPlatformTools wires NATS executors into the tool registry for each
// MVP agent. MVP platforms: Telegram (API), VK (API), Yandex.Business (RPA),
// Google Business (API). Each platform's tool list lives in a sibling
// tools_{platform}.go file to keep individual files under the 500-LOC
// budget; this dispatcher is the single registration entry point.
//
// Floor + EditableFields live on each spec — see toolregistry.ToolSpec for
// the policy rubric.
func RegisterPlatformTools(reg *toolregistry.Registry, nc *natslib.Conn) {
	agents := []struct {
		id    a2a.AgentID
		tools []toolregistry.ToolSpec
	}{
		{id: a2a.AgentTelegram, tools: telegramTools()},
		{id: a2a.AgentVK, tools: vkTools()},
		{id: a2a.AgentYandexBusiness, tools: yandexTools()},
		{id: a2a.AgentGoogleBusiness, tools: googleTools()},
	}

	conn := natsexec.NewNATSConn(nc)
	for _, a := range agents {
		for _, spec := range a.tools {
			exec := natsexec.New(a.id, spec.Def.Function.Name, conn)
			reg.Register(spec, exec)
		}
	}
}
