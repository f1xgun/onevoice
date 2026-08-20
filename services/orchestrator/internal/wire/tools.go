package wire

import (
	"context"
	"log/slog"
	"time"

	natslib "github.com/nats-io/nats.go"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/natsauth"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/reviewstats"
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
// ctx is used only to build the generated-media object store (a MinIO
// BucketExists/MakeBucket round trip) when image generation is enabled; the v1
// nats.go dial takes no context. billing is threaded into the in-process
// generate_image executor so image spend lands in usage_logs. mongoDB backs the
// read-only get_review_stats tool, which queries the shared reviews collection
// in-process (no NATS) — so it is registered regardless of NATS reachability.
func Tools(ctx context.Context, log *slog.Logger, cfg *config.Config, billing llm.Writer, mongoDB *mongo.Database) (*toolregistry.Registry, *natslib.Conn, error) {
	reg := toolregistry.NewRegistry()

	registerReviewStatsTool(log, reg, mongoDB)

	authOpts := natsauth.Options()
	dialOpts := make([]natslib.Option, 0, 5+len(authOpts))
	dialOpts = append(dialOpts,
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
	dialOpts = append(dialOpts, authOpts...)
	nc, err := natslib.Connect(cfg.NATSUrl, dialOpts...)
	if err != nil {
		// With RetryOnFailedConnect a failed initial dial surfaces as a
		// reconnecting conn, not an error — so an error here means an
		// options/URL problem. Degrade to an empty registry + nil conn
		// (cmd/main.go skips the NATS health check and Drain on nil) so the
		// process still serves /health instead of crash-looping.
		log.Warn("NATS connect failed", "url", cfg.NATSUrl, "error", err)
		return reg, nil, nil
	}

	RegisterPlatformTools(reg, nc, cfg.EnableGoogleBusiness)
	log.Info("registered platform tools", "nats_url", cfg.NATSUrl)

	registerImageGenTool(ctx, log, reg, cfg, billing)
	return reg, nc, nil
}

// registerReviewStatsTool wires the read-only get_review_stats tool against the
// shared reviews collection. A nil mongoDB (orchestrator booted without Mongo)
// leaves the tool unregistered rather than offering a handler that would fail
// every call.
func registerReviewStatsTool(log *slog.Logger, reg *toolregistry.Registry, mongoDB *mongo.Database) {
	if mongoDB == nil {
		return
	}
	RegisterReviewStatsTool(reg, reviewstats.NewMongoRepo(mongoDB))
	log.Info("registered internal tools", "get_review_stats", true)
}

// registerImageGenTool wires the in-process generate_image tool when image
// generation is enabled AND fully configured. It degrades gracefully: an
// unconfigured generator (nil) or an object store that fails to initialize
// leaves the tool unregistered and the process boots normally — the feature is
// strictly opt-in, so a partial config must never crash the orchestrator.
func registerImageGenTool(ctx context.Context, log *slog.Logger, reg *toolregistry.Registry, cfg *config.Config, billing llm.Writer) {
	gen := ImageGen(cfg)
	if gen == nil {
		return
	}
	store, err := ImageStore(ctx, cfg)
	if err != nil {
		log.Warn("generate_image tool disabled: object store init failed — check S3_ENDPOINT / PUBLIC_URL", "error", err)
		return
	}
	RegisterInternalTools(reg, gen, store, billing, cfg)
	log.Info("registered internal tools", "generate_image", true, "provider", gen.Name())
}

// RegisterPlatformTools wires NATS executors into the tool registry for each
// platform agent. MVP platforms: Telegram (API), VK (API), Yandex.Business
// (RPA). Google Business is gated behind enableGoogleBusiness (off by default)
// because it is unverified and hidden on the integrations UI — registering its
// tools unconditionally surfaces them as approvable in Settings → Tools for a
// platform that can never be connected. Each platform's tool list lives in a
// sibling tools_{platform}.go file to keep individual files under the 500-LOC
// budget; this dispatcher is the single registration entry point.
//
// Floor + EditableFields live on each spec — see toolregistry.ToolSpec for
// the policy rubric.
func RegisterPlatformTools(reg *toolregistry.Registry, nc *natslib.Conn, enableGoogleBusiness bool) {
	type platformAgent struct {
		id    a2a.AgentID
		tools []toolregistry.ToolSpec
	}
	agents := []platformAgent{
		{id: a2a.AgentTelegram, tools: telegramTools()},
		{id: a2a.AgentVK, tools: vkTools()},
		{id: a2a.AgentYandexBusiness, tools: yandexTools()},
	}
	if enableGoogleBusiness {
		agents = append(agents, platformAgent{id: a2a.AgentGoogleBusiness, tools: googleTools()})
	}

	conn := natsexec.NewNATSConn(nc)
	for _, a := range agents {
		for _, spec := range a.tools {
			exec := natsexec.New(a.id, spec.Def.Function.Name, conn)
			reg.Register(spec, exec)
		}
	}
}
