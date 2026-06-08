package wire

import (
	"log/slog"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// natsReconnectWait is the backoff between NATS reconnect attempts.
const natsReconnectWait = 2 * time.Second

// Tools constructs the live tool registry, dials NATS, and registers every MVP
// platform tool. The dial uses RetryOnFailedConnect + infinite reconnect, so a
// NATS that is down at boot is non-fatal: Connect returns a live conn already
// in the reconnecting state and the platform tools are registered regardless.
// Registration only builds tool specs + executors (it needs no live
// connection), and the executors hold the auto-reconnecting conn — so tool
// calls start succeeding as soon as NATS becomes reachable, with no
// orchestrator restart. This replaces the prior behavior where a boot-time
// NATS outage left the registry permanently empty.
//
// No ctx parameter: the v1 nats.go API does not accept a context for dial,
// so threading one through would be ceremonial.
func Tools(log *slog.Logger, cfg *config.Config) (*toolregistry.Registry, *natslib.Conn, error) {
	reg := toolregistry.NewRegistry()

	nc, err := natslib.Connect(cfg.NATSUrl,
		natslib.RetryOnFailedConnect(true),
		natslib.MaxReconnects(-1),
		natslib.ReconnectWait(natsReconnectWait),
		natslib.DisconnectErrHandler(func(_ *natslib.Conn, e error) {
			log.Warn("NATS disconnected", "error", e)
		}),
		natslib.ReconnectHandler(func(c *natslib.Conn) {
			log.Info("NATS reconnected", "url", c.ConnectedUrl())
		}),
	)
	if err != nil {
		// With RetryOnFailedConnect a failed initial dial surfaces as a
		// reconnecting conn, not an error — so an error here means an
		// options/URL problem. Degrade to an empty registry + nil conn
		// (cmd/main.go skips the NATS health check and Drain on nil) so the
		// process still serves /health instead of crash-looping.
		log.Warn("NATS connect failed", "url", cfg.NATSUrl, "error", err)
		return reg, nil, nil
	}

	RegisterPlatformTools(reg, nc)
	log.Info("registered platform tools", "nats_url", cfg.NATSUrl)
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

	registerToolAliases(reg)
}

// registerToolAliases maps known LLM-emitted name variants onto their canonical
// registered tools. Weaker models blend tool names across platforms — e.g.
// vk__send_post from Telegram's telegram__send_channel_post — which would
// otherwise fail-close to forbidden. See toolregistry.RegisterAlias.
func registerToolAliases(reg *toolregistry.Registry) {
	reg.RegisterAlias("vk__send_post", tools.VKPublishPost)
}
